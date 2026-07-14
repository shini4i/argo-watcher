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
// found to be superseded by a newer deployment for the same app. The retry loop
// re-checks this before each attempt and aborts rather than committing a stale
// image tag — so a larger retry budget cannot let an older deployment win over a
// newer one. Callers treat it as "cancelled", not a failure.
var ErrDeploymentSuperseded = errors.New("deployment superseded before write-back; aborting to avoid committing a stale image tag")

const (
	managedImagesAnnotation = "argo-watcher/managed-images"
	managedImageTagPattern  = "argo-watcher/%s.helm.image-tag"
)

// generateOverrideFileContent builds the Helm override file for the task's
// managed images from the app's annotations. It returns nil (no error) when no
// managed images are declared, and errors if a managed image is missing its
// tag-path annotation.
func generateOverrideFileContent(annotations map[string]string, task *models.Task) (*updater.ArgoOverrideFile, error) {
	overrideFileContent := updater.ArgoOverrideFile{}
	managedImages, err := extractManagedImages(annotations)
	if err != nil {
		return nil, err
	}

	if len(managedImages) == 0 {
		slog.Error(fmt.Sprintf("%s annotation not found, skipping image update", managedImagesAnnotation))
		return nil, nil
	}

	for _, image := range task.Images {
		for appAlias, appImage := range managedImages {
			if image.Image == appImage {
				tagAnnotation := fmt.Sprintf(managedImageTagPattern, appAlias)
				tagPath, exists := annotations[tagAnnotation]
				if !exists {
					// The image is declared managed but has no tag-path annotation, so
					// we cannot know which Helm value to override. Silently skipping here
					// would let the write-back report success while never updating git;
					// fail loudly so the misconfiguration surfaces on the task instead.
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

	return &overrideFileContent, nil
}

// UpdateGitImageTag writes the new image tag for app into the GitOps repository.
// gitHandler is injected to enable testing; production callers pass updater.GitClient{}.
//
// ctx is propagated into the retry loop and each per-attempt context so that
// a caller can cancel in-flight git operations (e.g. on graceful shutdown).
// Production callers that lack a context chain may pass context.Background()
// until a proper context is wired through the call stack.
//
// isSuperseded is an optional (at most one) predicate re-checked before each
// write-back attempt; when it returns true the loop aborts with
// ErrDeploymentSuperseded instead of committing, so a newer deployment for the
// same app is never overwritten by an older one that keeps retrying.
func UpdateGitImageTag(ctx context.Context, app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo, gitHandler updater.GitHandler, isSuperseded ...func() bool) error {
	if gitopsRepo.Path == "" {
		slog.Warn(fmt.Sprintf("No path found for app %s, unsupported Application configuration", app.Metadata.Name), "id", task.Id)
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
		slog.Warn(fmt.Sprintf("No release overrides found for app %s", app.Metadata.Name), "id", task.Id)
		return nil
	}

	repo, err := updater.NewGitRepo(gitopsRepo.RepoUrl, gitopsRepo.BranchName, gitopsRepo.Path, gitopsRepo.Filename, gitopsRepo.RepoCachePath, gitHandler)
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to create git repo instance for %s: %s", gitopsRepo.RepoUrl, err), "id", task.Id)
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
// number of the attempt that just failed. It computes base*2^(attempt-1),
// capped at gitUpdateMaxBackoff, then applies full jitter (a uniform random
// value in [0, ceiling]). Full jitter keeps early retries tight — the key to
// winning a push race under sustained concurrent-writer contention — and also
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

// runGitUpdateWithRetry runs the clone+update sequence with per-attempt
// bounded contexts and a fixed-backoff retry loop. The final attempt always
// invalidates the on-disk cache before running so a poisoned cache (partial
// commit, stale ref, half-written file) is replaced by a fresh clone and
// self-heals without operator intervention — regardless of the attempt count.
//
// Each attempt derives its context from parentCtx via WithTimeout(opTimeout),
// so one stuck attempt cannot consume the budget of subsequent attempts. If
// parentCtx is already cancelled, the loop exits early without sleeping.
// The total worst-case wall clock is GitOpTimeout * GitMaxAttempts plus the
// sum of the inter-attempt backoffs, each bounded by gitUpdateMaxBackoff.
//
// Permanent errors (see updater.IsPermanent) short-circuit the loop — for
// example, a bad SSH key or auth failure will fail the same way on every
// attempt and retrying just wastes the budget.
func runGitUpdateWithRetry(parentCtx context.Context, repo *updater.GitRepo, appName string, releaseOverrides *updater.ArgoOverrideFile, task *models.Task, isSuperseded func() bool) error {
	maxAttempts := repo.GitMaxAttempts()
	opTimeout := repo.GitOpTimeout()

	var lastErr error
	for attempt := uint(1); attempt <= maxAttempts; attempt++ {
		// Abort before touching git if a newer deployment for the same app has
		// superseded this task. Re-checked every attempt (not just once up front)
		// so a task that keeps retrying under contention cannot commit a stale tag
		// on top of a newer one — the correctness guard that makes a larger retry
		// budget safe.
		if isSuperseded != nil && isSuperseded() {
			slog.Info(fmt.Sprintf("Git update aborted at attempt %d/%d: task superseded by a newer deployment", attempt, maxAttempts), "id", task.Id)
			return ErrDeploymentSuperseded
		}

		invalidateCacheOnFinalAttempt(repo, task, attempt, maxAttempts)

		err := runGitUpdateAttempt(parentCtx, repo, opTimeout, appName, releaseOverrides, task)
		if err == nil {
			if attempt > 1 {
				slog.Info(fmt.Sprintf("Git update succeeded on attempt %d/%d", attempt, maxAttempts), "id", task.Id)
			}
			return nil
		}
		lastErr = err

		if updater.IsPermanent(err) {
			slog.Error(fmt.Sprintf("Git update failed with permanent error on attempt %d/%d; not retrying", attempt, maxAttempts), "error", err, "id", task.Id)
			return err
		}

		if waitErr := backoffBeforeRetry(parentCtx, task, err, attempt, maxAttempts); waitErr != nil {
			return waitErr
		}
	}

	return fmt.Errorf("git update failed after %d attempts: %w", maxAttempts, lastErr)
}

// invalidateCacheOnFinalAttempt clears the on-disk cache before the final
// attempt so a poisoned cache (partial commit, stale ref, half-written file) is
// replaced by a fresh clone and self-heals without operator intervention.
func invalidateCacheOnFinalAttempt(repo *updater.GitRepo, task *models.Task, attempt, maxAttempts uint) {
	if attempt != maxAttempts {
		return
	}
	slog.Warn(fmt.Sprintf(
		"Final attempt %d/%d: invalidating cache and performing fresh clone",
		attempt, maxAttempts,
	), "id", task.Id)
	if invErr := repo.InvalidateCache(); invErr != nil {
		slog.Warn("Failed to invalidate cache before final attempt; proceeding anyway", "error", invErr, "id", task.Id)
	}
}

// backoffBeforeRetry waits (jittered) before the next attempt, unless this was
// the final one. It returns a non-nil error only if parentCtx is cancelled
// during the wait, signalling the caller to stop retrying.
func backoffBeforeRetry(parentCtx context.Context, task *models.Task, attemptErr error, attempt, maxAttempts uint) error {
	if attempt >= maxAttempts {
		return nil
	}
	backoff := gitUpdateBackoff(attempt)
	slog.Warn(fmt.Sprintf(
		"Git update attempt %d/%d failed; retrying after %s",
		attempt, maxAttempts, backoff,
	), "error", attemptErr, "id", task.Id)
	select {
	case <-parentCtx.Done():
		return fmt.Errorf("git update cancelled during backoff: %w", parentCtx.Err())
	case <-time.After(backoff):
		return nil
	}
}

// runGitUpdateAttempt performs one clone+update cycle. It derives a per-attempt
// context from parentCtx bounded by opTimeout, so a hung network call is
// capped at opTimeout while still honouring cancellation from the parent.
// Errors are returned wrapped with the failed phase so the retry loop's logs
// indicate where the attempt failed.
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

// extractManagedImages extracts the managed images from the application's annotations.
// It returns a map of the managed images, where the key is the application alias and the value is the image name.
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
