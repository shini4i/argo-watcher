package models

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/pkg/updater"
)

// ErrDeploymentSuperseded is returned by the git write-back when the task is
// found to be superseded by a newer deployment for the same app. The retry loop
// re-checks this before each attempt and aborts rather than committing a stale
// image tag — so a larger retry budget cannot let an older deployment win over a
// newer one. Callers treat it as "cancelled", not a failure.
var ErrDeploymentSuperseded = errors.New("deployment superseded before write-back; aborting to avoid committing a stale image tag")

const (
	ArgoRolloutAppSuccess      = "success"
	ArgoRolloutAppNotSynced    = "not synced"
	ArgoRolloutAppNotAvailable = "not available"
	ArgoRolloutAppNotHealthy   = "not healthy"
	ArgoRolloutAppDegraded     = "degraded"
)

const (
	managedAnnotation       = "argo-watcher/managed"
	managedImagesAnnotation = "argo-watcher/managed-images"
	managedImageTagPattern  = "argo-watcher/%s.helm.image-tag"
	managedGitRepo          = "argo-watcher/write-back-repo"
	managedGitBranch        = "argo-watcher/write-back-branch"
	managedGitPath          = "argo-watcher/write-back-path"
	managedGitFile          = "argo-watcher/write-back-filename"
	fireAndForgetAnnotation = "argo-watcher/fire-and-forget"
)

type ApplicationOperationResource struct {
	HookPhase string `json:"hookPhase"` // example: Failed
	HookType  string `json:"hookType"`  // example: PreSync
	Kind      string `json:"kind"`      // example: Pod | Job
	Message   string `json:"message"`   // example: Job has reached the specified backoff limit
	Status    string `json:"status"`    // example: Synced
	SyncPhase string `json:"syncPhase"` // example: PreSync
	Name      string `json:"name"`      // example: app-migrations
	Namespace string `json:"namespace"` // example: app
}

type ApplicationResource struct {
	Kind      string `json:"kind"`      // example: Pod | Job
	Name      string `json:"name"`      // example: app-migrations
	Namespace string `json:"namespace"` // example: app
	Health    struct {
		Message string `json:"message"` // example: Job has reached the specified backoff limit
		Status  string `json:"status"`  // example: Synced
	} `json:"health"`
}

type Application struct {
	Metadata ApplicationMetadata `json:"metadata"`
	Spec     ApplicationSpec     `json:"spec"`
	Status   ApplicationStatus   `json:"status"`
}

type ApplicationStatus struct {
	Health struct {
		Status string `json:"status"`
	}
	OperationState ApplicationStatusOperationState `json:"operationState"`
	Resources      []ApplicationResource           `json:"resources"`
	Summary        struct {
		Images []string `json:"images"`
	}
	Sync struct {
		Status string `json:"status"`
	}
}

type ApplicationStatusOperationState struct {
	Phase      string `json:"phase"`
	Message    string `json:"message"`
	SyncResult struct {
		Resources []ApplicationOperationResource `json:"resources"`
	} `json:"syncResult"`
}

type ApplicationMetadata struct {
	Name        string            `json:"name"`
	Annotations map[string]string `json:"annotations"`
}

type ApplicationSpec struct {
	Source  ApplicationSource   `json:"source"`
	Sources []ApplicationSource `json:"sources"`
}

type ApplicationSource struct {
	RepoURL        string `json:"repoURL"`
	TargetRevision string `json:"targetRevision"`
	Path           string `json:"path"`
}

// GetRolloutStatus calculates application rollout status depending on the expected images and proxy configuration.
func (app *Application) GetRolloutStatus(rolloutImages []string, registryProxyUrl string, acceptSuspended bool) string {
	// check if all the images rolled out
	for _, image := range rolloutImages {
		if !helpers.ImagesContains(app.Status.Summary.Images, image, registryProxyUrl) {
			return ArgoRolloutAppNotAvailable
		}
	}

	// if an application reached the degraded status, we can stop processing the task
	if app.Status.Health.Status == "Degraded" && app.Status.Sync.Status != "OutOfSync" {
		return ArgoRolloutAppDegraded
	}

	// verify app sync status
	if app.Status.Sync.Status != "Synced" {
		return ArgoRolloutAppNotSynced
	}

	// an optional check that helps when we are dealing with Rollout object that can be in a suspended state
	// during the rollout process
	if app.Status.Health.Status == "Suspended" && app.Status.Sync.Status == "Synced" && acceptSuspended {
		return ArgoRolloutAppSuccess
	}

	// verify app health status
	if app.Status.Health.Status != "Healthy" {
		return ArgoRolloutAppNotHealthy
	}

	// all good
	return ArgoRolloutAppSuccess
}

// GetRolloutMessage generates rollout failure message.
func (app *Application) GetRolloutMessage(status string, rolloutImages []string) string {
	// handle application sync failure
	switch status {
	// not all images were deployed to the application
	case ArgoRolloutAppNotAvailable:
		// Image-mismatch is the bare minimum we always show. We additionally append any actionable diagnostics
		// already returned by ArgoCD (failed sync operation, failed hooks, unhealthy resources) so on-call users
		// don't have to context-switch into the ArgoCD UI to find the root cause.
		base := fmt.Sprintf(
			"List of current images (last app check):\n"+
				"\t%s\n\n"+
				"List of expected images:\n"+
				"\t%s",
			strings.Join(app.Status.Summary.Images, "\n\t"),
			strings.Join(rolloutImages, "\n\t"),
		)
		if diag := app.buildNotAvailableDiagnostics(); diag != "" {
			return base + "\n\n" + diag
		}
		return base
	// application sync status wasn't valid
	case ArgoRolloutAppNotSynced:
		// display sync status and last sync message
		return fmt.Sprintf(
			"App status \"%s\"\n"+
				"App message \"%s\"\n"+
				"Resources:\n"+
				"\t%s",
			app.Status.OperationState.Phase,
			app.Status.OperationState.Message,
			strings.Join(app.ListSyncResultResources(), "\n\t"),
		)
	// application is not in a healthy status
	case ArgoRolloutAppNotHealthy, ArgoRolloutAppDegraded:
		// display current health of pods
		return fmt.Sprintf(
			"App sync status \"%s\"\n"+
				"App health status \"%s\"\n"+
				"Resources:\n"+
				"\t%s",
			app.Status.Sync.Status,
			app.Status.Health.Status,
			strings.Join(app.ListUnhealthyResources(), "\n\t"),
		)
	}

	// handle unexpected status
	return fmt.Sprintf(
		"received unexpected rollout status \"%s\"",
		status,
	)
}

// formatSyncResultResource renders a single sync-result resource line. Shared between full and filtered listings
// so the user-facing failure-report format stays consistent if it ever changes.
func formatSyncResultResource(r ApplicationOperationResource) string {
	return fmt.Sprintf("%s(%s) %s %s with message %s", r.Kind, r.Name, r.HookType, r.HookPhase, r.Message)
}

// ListSyncResultResources returns a list of strings representing the sync result resources of the application.
// Each string in the list contains information about the resource's kind, name, hook type, hook phase, and message.
// The information is formatted as "{kind}({name}) {hookType} {hookPhase} with message {message}".
// The list is generated based on the Application's status and its operation state's sync result resources.
func (app *Application) ListSyncResultResources() []string {
	list := make([]string, len(app.Status.OperationState.SyncResult.Resources))
	for index := range app.Status.OperationState.SyncResult.Resources {
		list[index] = formatSyncResultResource(app.Status.OperationState.SyncResult.Resources[index])
	}
	return list
}

// formatHealthResource renders a single health-bearing resource line. Shared between full and filtered listings
// so the user-facing failure-report format stays consistent if it ever changes.
func formatHealthResource(r ApplicationResource) string {
	line := fmt.Sprintf("%s(%s) %s", r.Kind, r.Name, r.Health.Status)
	if r.Health.Message != "" {
		line += " with message " + r.Health.Message
	}
	return line
}

// ListUnhealthyResources returns a list of strings representing the unhealthy resources of the application.
// Each string in the list contains information about the resource's kind, name, and health status.
// If available, the resource's health message is also included in the string.
// The format of each string is "{kind}({name}) {status}" or "{kind}({name}) {status} with message {message}".
// The list is generated based on the Application's status and its resources with non-empty health status.
func (app *Application) ListUnhealthyResources() []string {
	var list []string

	for index := range app.Status.Resources {
		resource := app.Status.Resources[index]
		if resource.Health.Status == "" {
			continue
		}
		list = append(list, formatHealthResource(resource))
	}
	return list
}

// isTerminalFailurePhase reports whether the given ArgoCD phase value indicates a terminal failure. The same
// predicate is applied to both Status.OperationState.Phase (OperationPhase) and SyncResult.Resources[].HookPhase
// (HookPhase); upstream defines these as separate string enums that share the same value set.
// "Running", "Succeeded", "Terminating", and the empty string are deliberately excluded.
func isTerminalFailurePhase(phase string) bool {
	return phase == "Failed" || phase == "Error"
}

// isProblemHealthStatus reports whether an ArgoCD resource HealthStatusCode indicates a problem worth surfacing
// in the deployment failure report. "Healthy" and "Progressing" are excluded so they don't dilute the signal;
// "Synced" appears in legacy fixtures and is treated as non-actionable.
func isProblemHealthStatus(status string) bool {
	switch status {
	case "Degraded", "Missing", "Unknown", "Suspended":
		return true
	default:
		return false
	}
}

// listFailedSyncResultResources returns formatted lines for sync-result resources whose hook phase indicates failure.
// Reuses the same one-line format as ListSyncResultResources via formatSyncResultResource.
func (app *Application) listFailedSyncResultResources() []string {
	var list []string
	for index := range app.Status.OperationState.SyncResult.Resources {
		resource := app.Status.OperationState.SyncResult.Resources[index]
		if !isTerminalFailurePhase(resource.HookPhase) {
			continue
		}
		list = append(list, formatSyncResultResource(resource))
	}
	return list
}

// listProblemResources returns formatted lines for resources whose health status indicates a problem.
// Reuses formatHealthResource for output formatting; applies the stricter problem-only filter via isProblemHealthStatus.
func (app *Application) listProblemResources() []string {
	var list []string
	for index := range app.Status.Resources {
		resource := app.Status.Resources[index]
		if !isProblemHealthStatus(resource.Health.Status) {
			continue
		}
		list = append(list, formatHealthResource(resource))
	}
	return list
}

// buildNotAvailableDiagnostics builds the optional diagnostics suffix appended to the "not available" rollout
// failure message. Each section is included only when it has content; the empty string is returned when no
// diagnostics are available, preserving the legacy image-list-only output for that case.
func (app *Application) buildNotAvailableDiagnostics() string {
	var sections []string

	if isTerminalFailurePhase(app.Status.OperationState.Phase) {
		opSection := fmt.Sprintf("Sync operation phase: %s", app.Status.OperationState.Phase)
		if msg := app.Status.OperationState.Message; msg != "" {
			opSection += "\nSync operation message: " + msg
		}
		sections = append(sections, opSection)
	}

	if hooks := app.listFailedSyncResultResources(); len(hooks) > 0 {
		sections = append(sections, "Failed hooks:\n\t"+strings.Join(hooks, "\n\t"))
	}

	if resources := app.listProblemResources(); len(resources) > 0 {
		sections = append(sections, "Unhealthy resources:\n\t"+strings.Join(resources, "\n\t"))
	}

	return strings.Join(sections, "\n\n")
}

// IsManagedByWatcher checks if the application is managed by the watcher.
// It checks if the application's metadata contains the "argo-watcher/managed" annotation with the value "true".
func (app *Application) IsManagedByWatcher() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[managedAnnotation] == "true"
}

func (app *Application) generateOverrideFileContent(annotations map[string]string, task *Task) (*updater.ArgoOverrideFile, error) {
	overrideFileContent := updater.ArgoOverrideFile{}
	managedImages, err := extractManagedImages(annotations)
	if err != nil {
		return nil, err
	}

	if len(managedImages) == 0 {
		log.Error().Msgf("%s annotation not found, skipping image update", managedImagesAnnotation)
		return nil, nil
	}

	for _, image := range task.Images {
		for appAlias, appImage := range managedImages {
			if image.Image == appImage {
				if tagPath, exists := annotations[fmt.Sprintf(managedImageTagPattern, appAlias)]; exists {
					overrideFileContent.Helm.Parameters = append(overrideFileContent.Helm.Parameters, updater.ArgoParameterOverride{
						Name:        tagPath,
						Value:       image.Tag,
						ForceString: true,
					})
				} else {
					log.Error().Msgf("%s annotation not found, skipping image %s update", fmt.Sprintf(managedImageTagPattern, appAlias), image.Image)
				}
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
func (app *Application) UpdateGitImageTag(ctx context.Context, task *Task, gitopsRepo *GitopsRepo, gitHandler updater.GitHandler, isSuperseded ...func() bool) error {
	if gitopsRepo.Path == "" {
		log.Warn().Str("id", task.Id).Msgf("No path found for app %s, unsupported Application configuration", app.Metadata.Name)
		return nil
	}

	var supersededCheck func() bool
	if len(isSuperseded) > 0 {
		supersededCheck = isSuperseded[0]
	}

	releaseOverrides, err := app.generateOverrideFileContent(app.Metadata.Annotations, task)
	if err != nil {
		return err
	}

	if releaseOverrides == nil {
		log.Warn().Str("id", task.Id).Msgf("No release overrides found for app %s", app.Metadata.Name)
		return nil
	}

	repo, err := updater.NewGitRepo(gitopsRepo.RepoUrl, gitopsRepo.BranchName, gitopsRepo.Path, gitopsRepo.Filename, gitopsRepo.RepoCachePath, gitHandler)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to create git repo instance for %s: %s", gitopsRepo.RepoUrl, err)
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
	// #nosec G404 -- this jitter only de-synchronises retries (anti-thundering-herd);
	// it guards no secret and gates no security decision, so a non-crypto RNG is correct.
	return time.Duration(rand.Int63n(int64(ceiling) + 1))
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
func runGitUpdateWithRetry(parentCtx context.Context, repo *updater.GitRepo, appName string, releaseOverrides *updater.ArgoOverrideFile, task *Task, isSuperseded func() bool) error {
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
			log.Info().Str("id", task.Id).Msgf("Git update aborted at attempt %d/%d: task superseded by a newer deployment", attempt, maxAttempts)
			return ErrDeploymentSuperseded
		}

		// Always invalidate the cache on the final attempt. A poisoned cache may
		// originate from a prior task on a previous invocation, not just the
		// current attempt, so the self-heal must fire unconditionally.
		if attempt == maxAttempts {
			log.Warn().Str("id", task.Id).Msgf(
				"Final attempt %d/%d: invalidating cache and performing fresh clone",
				attempt, maxAttempts,
			)
			if invErr := repo.InvalidateCache(); invErr != nil {
				log.Warn().Err(invErr).Str("id", task.Id).Msg("Failed to invalidate cache before final attempt; proceeding anyway")
			}
		}

		err := runGitUpdateAttempt(parentCtx, repo, opTimeout, appName, releaseOverrides, task)
		if err == nil {
			if attempt > 1 {
				log.Info().Str("id", task.Id).Msgf("Git update succeeded on attempt %d/%d", attempt, maxAttempts)
			}
			return nil
		}

		lastErr = err

		if updater.IsPermanent(err) {
			log.Error().Err(err).Str("id", task.Id).Msgf("Git update failed with permanent error on attempt %d/%d; not retrying", attempt, maxAttempts)
			return err
		}

		if attempt < maxAttempts {
			backoff := gitUpdateBackoff(attempt)
			log.Warn().Err(err).Str("id", task.Id).Msgf(
				"Git update attempt %d/%d failed; retrying after %s",
				attempt, maxAttempts, backoff,
			)
			select {
			case <-parentCtx.Done():
				return fmt.Errorf("git update cancelled during backoff: %w", parentCtx.Err())
			case <-time.After(backoff):
			}
		}
	}

	return fmt.Errorf("git update failed after %d attempts: %w", maxAttempts, lastErr)
}

// runGitUpdateAttempt performs one clone+update cycle. It derives a per-attempt
// context from parentCtx bounded by opTimeout, so a hung network call is
// capped at opTimeout while still honouring cancellation from the parent.
// Errors are returned wrapped with the failed phase so the retry loop's logs
// indicate where the attempt failed.
func runGitUpdateAttempt(parentCtx context.Context, repo *updater.GitRepo, opTimeout time.Duration, appName string, releaseOverrides *updater.ArgoOverrideFile, task *Task) error {
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

// IsFireAndForgetModeActive checks if 'fire-and-forget' mode is enabled in Application's annotations.
func (app *Application) IsFireAndForgetModeActive() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[fireAndForgetAnnotation] == "true"
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

type Userinfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username"`
}
