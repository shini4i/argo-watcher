package argocd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// ErrDeploymentSuperseded is returned by the git write-back when the task is
// superseded by a newer deployment for the same app, so a larger retry budget
// cannot let an older deployment win over a newer one. Callers treat it as
// "cancelled", not a failure.
var ErrDeploymentSuperseded = errors.New("deployment superseded before write-back; aborting to avoid committing a stale image tag")

const (
	managedImagesAnnotation = "argo-watcher/managed-images"
	managedImageTagPattern  = "argo-watcher/%s.helm.image-tag"
)

// generateOverrideFileContent builds the Helm override file for the task's managed
// images. It returns nil (no error) when no managed images are declared.
func generateOverrideFileContent(annotations map[string]string, task *models.Task) (*updater.ArgoOverrideFile, error) {
	overrideFileContent := updater.ArgoOverrideFile{}
	managedImages, err := extractManagedImages(annotations)
	if err != nil {
		return nil, err
	}

	if len(managedImages) == 0 {
		slog.Warn("annotation not found, skipping image update", "annotation", managedImagesAnnotation)
		return nil, nil
	}

	for _, image := range task.Images {
		for appAlias, appImage := range managedImages {
			if image.Image == appImage {
				tagAnnotation := fmt.Sprintf(managedImageTagPattern, appAlias)
				tagPath, exists := annotations[tagAnnotation]
				if !exists {
					// Without the tag-path annotation we cannot know which Helm value to
					// override. Silently skipping would let the write-back report success
					// while never updating git, so fail loudly instead.
					return nil, fmt.Errorf("managed image %q (alias %q) is missing its %s annotation", appImage, appAlias, tagAnnotation)
				}
				overrideFileContent.Helm.Parameters = append(overrideFileContent.Helm.Parameters, updater.ArgoParameterOverride{
					Name:        tagPath,
					Value:       image.Tag,
					ForceString: true,
				})
			}
		}
	}

	// No image the task carries is managed by this application, so there is no tag
	// to write. Returning the empty override would clone the repository to write
	// nothing, and create an override file holding an empty parameter list.
	if len(overrideFileContent.Helm.Parameters) == 0 {
		slog.Warn("no task image matches a managed image, skipping write-back",
			"annotation", managedImagesAnnotation, "id", task.Id)
		return nil, nil
	}

	return &overrideFileContent, nil
}

// UpdateGitImageTag writes the new image tag for app into the GitOps repository.
// gitHandler is injected to enable testing; production callers pass updater.GitClient{}.
//
// Cancelling ctx cancels in-flight git operations (e.g. on graceful shutdown).
// Production callers that lack a context chain may pass context.Background()
// until a proper context is wired through the call stack.
//
// isSuperseded is an optional (at most one) predicate re-checked before each
// write-back attempt; when it returns true the loop aborts with
// ErrDeploymentSuperseded instead of committing.
func UpdateGitImageTag(ctx context.Context, app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo, gitHandler updater.GitHandler, isSuperseded ...func() bool) error {
	if gitopsRepo.Path == "" {
		slog.Warn("No path found for app, unsupported Application configuration", "app", app.Metadata.Name, "id", task.Id)
		return nil
	}

	var supersededCheck func() bool
	if len(isSuperseded) > 0 {
		supersededCheck = isSuperseded[0]
	}

	releaseOverrides, err := generateOverrideFileContent(app.Metadata.Annotations, task)
	if err != nil {
		return err
	}

	if releaseOverrides == nil {
		slog.Warn("No release overrides found for app", "app", app.Metadata.Name, "id", task.Id)
		return nil
	}

	repo, err := updater.NewGitRepo(gitopsRepo.RepoUrl, gitopsRepo.BranchName, gitopsRepo.Path, gitopsRepo.Filename, gitopsRepo.RepoCachePath, gitHandler)
	if err != nil {
		slog.Error("Failed to create git repo instance", "url", gitopsRepo.RepoUrl, "error", err, "id", task.Id)
		return err
	}

	return runGitUpdateWithRetry(ctx, repo, app.Metadata.Name, releaseOverrides, task, supersededCheck)
}

// Retry backoff bounds for the clone+update sequence. Backoff is capped
// exponential with full jitter (see gitUpdateBackoff): the first retries fire
// fast so the write-back can win a git push race against a competing writer
// before it advances the branch again, while later retries back off for a
// genuinely unavailable remote.
const (
	gitUpdateBaseBackoff = 250 * time.Millisecond
	gitUpdateMaxBackoff  = 2 * time.Second
)

// gitUpdateBackoff returns the delay before the next retry, given the 1-based
// number of the attempt that just failed. The full jitter it applies also
// de-synchronises multiple argo-watcher instances contending on one repo.
func gitUpdateBackoff(attempt uint) time.Duration {
	ceiling := gitUpdateBaseBackoff << (attempt - 1)
	// Guard the shift: a large attempt count overflows to <= 0; saturate at cap.
	if ceiling <= 0 || ceiling > gitUpdateMaxBackoff {
		ceiling = gitUpdateMaxBackoff
	}
	// math/rand is deliberate: this jitter only de-synchronises retries
	// (anti-thundering-herd). It guards no secret and gates no security
	// decision, so a non-crypto RNG is correct.
	// #nosec G404
	return time.Duration(rand.Int63n(int64(ceiling) + 1)) // NOSONAR: pseudorandom is safe here (retry jitter, not a security context)
}

// runGitUpdateWithRetry runs the clone+update sequence with per-attempt bounded
// contexts and a retry loop. The final attempt always invalidates the on-disk cache,
// so a poisoned cache (partial commit, stale ref, half-written file) self-heals.
//
// One stuck attempt cannot consume the budget of subsequent attempts. If parentCtx
// is already cancelled, the loop exits early without sleeping. The total worst-case
// wall clock is GitOpTimeout * GitMaxAttempts plus the sum of the inter-attempt
// backoffs, each bounded by gitUpdateMaxBackoff.
//
// Permanent errors (see updater.IsPermanent) short-circuit the loop — a bad SSH key
// or auth failure fails the same way on every attempt.
func runGitUpdateWithRetry(parentCtx context.Context, repo *updater.GitRepo, appName string, releaseOverrides *updater.ArgoOverrideFile, task *models.Task, isSuperseded func() bool) error {
	maxAttempts := repo.GitMaxAttempts()
	opTimeout := repo.GitOpTimeout()

	var lastErr error
	for attempt := uint(1); attempt <= maxAttempts; attempt++ {
		if isSuperseded != nil && isSuperseded() {
			slog.Info("Git update aborted: the task was superseded, taken over, or this replica is shutting down", "attempt", attempt, "max_attempts", maxAttempts, "id", task.Id)
			return ErrDeploymentSuperseded
		}

		invalidateCacheOnFinalAttempt(repo, task, attempt, maxAttempts)

		err := runGitUpdateAttempt(parentCtx, repo, opTimeout, appName, releaseOverrides, task)
		if err == nil {
			if attempt > 1 {
				slog.Info("Git update succeeded", "attempt", attempt, "max_attempts", maxAttempts, "id", task.Id)
			}
			return nil
		}
		lastErr = err

		if updater.IsPermanent(err) {
			slog.Error("Git update failed with permanent error; not retrying", "attempt", attempt, "max_attempts", maxAttempts, "error", err, "id", task.Id)
			return err
		}

		if waitErr := backoffBeforeRetry(parentCtx, task, err, attempt, maxAttempts); waitErr != nil {
			return waitErr
		}
	}

	return fmt.Errorf("git update failed after %d attempts: %w", maxAttempts, lastErr)
}

func invalidateCacheOnFinalAttempt(repo *updater.GitRepo, task *models.Task, attempt, maxAttempts uint) {
	if attempt != maxAttempts {
		return
	}
	slog.Warn("Final attempt: invalidating cache and performing fresh clone", "attempt", attempt, "max_attempts", maxAttempts, "id", task.Id)
	if invErr := repo.InvalidateCache(); invErr != nil {
		slog.Warn("Failed to invalidate cache before final attempt; proceeding anyway", "error", invErr, "id", task.Id)
	}
}

func backoffBeforeRetry(parentCtx context.Context, task *models.Task, attemptErr error, attempt, maxAttempts uint) error {
	if attempt >= maxAttempts {
		return nil
	}
	backoff := gitUpdateBackoff(attempt)
	slog.Warn("Git update attempt failed; retrying", "attempt", attempt, "max_attempts", maxAttempts, "backoff", backoff, "error", attemptErr, "id", task.Id)
	select {
	case <-parentCtx.Done():
		return fmt.Errorf("git update cancelled during backoff: %w", parentCtx.Err())
	case <-time.After(backoff):
		return nil
	}
}

// runGitUpdateAttempt performs one clone+update cycle, bounded by opTimeout.
func runGitUpdateAttempt(parentCtx context.Context, repo *updater.GitRepo, opTimeout time.Duration, appName string, releaseOverrides *updater.ArgoOverrideFile, task *models.Task) error {
	ctx, cancel := context.WithTimeout(parentCtx, opTimeout)
	defer cancel()

	if err := repo.Clone(ctx); err != nil {
		return fmt.Errorf("clone failed: %w", err)
	}

	if err := repo.UpdateApp(ctx, appName, releaseOverrides, task); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	return nil
}

// extractManagedImages maps each application alias from the annotations to its image name.
func extractManagedImages(annotations map[string]string) (map[string]string, error) {
	managedImages := map[string]string{}

	for annotation, value := range annotations {
		if annotation == managedImagesAnnotation {
			for _, image := range strings.Split(value, ",") {
				if !strings.Contains(image, "=") {
					return nil, fmt.Errorf("invalid format for %s annotation", managedImagesAnnotation)
				}
				managedImage := strings.Split(strings.TrimSpace(image), "=")
				managedImages[managedImage[0]] = managedImage[1]
			}
		}
	}

	return managedImages, nil
}
