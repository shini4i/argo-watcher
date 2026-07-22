package argocd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"

	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	// legacyRetryIntervals preserves the historical 15-step retry window (assumes 15s retry delay, totaling ~3m45s).
	legacyRetryIntervals = 15
)

// errForceRetry is an internal sentinel used to keep retry-go polling while the rollout has not reached a final state.
// It must never reach the user-visible task status — WaitRollout swallows it so the caller can report the actual rollout state instead.
var errForceRetry = errors.New("force retry")

// errTaskSuperseded is an internal sentinel returned by the poll loop when the task
// has been marked cancelled in the shared state by a newer deployment for the same
// app. Unlike errForceRetry it is not swallowed: WaitForRollout uses it to stop
// without overwriting the "cancelled" status the newer deployment already wrote.
var errTaskSuperseded = errors.New("task superseded by a newer deployment")

// DeploymentMonitor encapsulates the logic for tracking ArgoCD application rollouts.
type DeploymentMonitor struct {
	argo             Argo
	registryProxyUrl string
	retryOptions     []retry.Option
	acceptSuspended  bool
	retryDelay       time.Duration
	defaultAttempts  uint
	// refreshApp is the instance-wide default for requesting an ArgoCD refresh during status checks.
	// A per-task Refresh override takes precedence (see resolveRefresh).
	refreshApp bool
}

// NewDeploymentMonitor creates a deployment monitor with the supplied configuration.
func NewDeploymentMonitor(argo Argo, registryProxyUrl string, retryOptions []retry.Option, acceptSuspended bool, retryDelay time.Duration) *DeploymentMonitor {
	return &DeploymentMonitor{
		argo:             argo,
		registryProxyUrl: registryProxyUrl,
		retryOptions:     retryOptions,
		acceptSuspended:  acceptSuspended,
		retryDelay:       retryDelay,
	}
}

// BeginTracking increments the in-progress task counter.
func (monitor *DeploymentMonitor) BeginTracking() {
	monitor.argo.metrics.AddInProgressTask()
}

// EndTracking decrements the in-progress task counter.
func (monitor *DeploymentMonitor) EndTracking() {
	monitor.argo.metrics.RemoveInProgressTask()
}

// ObserveDeploymentDuration records the wall-clock duration of a successful deployment for the app.
func (monitor *DeploymentMonitor) ObserveDeploymentDuration(app string, seconds float64) {
	monitor.argo.metrics.ObserveDeploymentDuration(app, seconds)
}

// resolveRefresh reports whether this task's status check should request an ArgoCD refresh.
// A per-task Refresh override wins over the instance-wide default; a nil override (field omitted
// by the client) keeps the default, so old clients behave exactly as before (issue #334).
func (monitor *DeploymentMonitor) resolveRefresh(task models.Task) bool {
	if task.Refresh != nil {
		return *task.Refresh
	}
	return monitor.refreshApp
}

// FetchApplication retrieves the ArgoCD application by name. The context bounds the underlying
// API call so callers polling under a deadline are not blocked past it.
//
// When a refresh is requested, the call is timed and its duration recorded (argocd_refresh_duration_seconds)
// so slow or stuck refreshes are diagnosable. A stuck refresh is avoided operationally, not here: set the
// per-task Refresh override (or ARGO_REFRESH_APP) to false for apps that never settle (issue #334).
func (monitor *DeploymentMonitor) FetchApplication(ctx context.Context, appName string, refresh bool) (*models.Application, error) {
	if !refresh {
		return monitor.argo.api.GetApplication(ctx, appName, false)
	}

	start := time.Now()
	app, err := monitor.argo.api.GetApplication(ctx, appName, true)
	monitor.argo.metrics.ObserveRefreshDuration(appName, time.Since(start).Seconds())
	return app, err
}

// StoreInitialAppStatus caches the initial rollout status for comparison during monitoring.
func (monitor *DeploymentMonitor) StoreInitialAppStatus(task *models.Task, application *models.Application) error {
	if application == nil {
		return errors.New("application is nil")
	}

	status := application.GetRolloutStatus(task.ListImages(), monitor.registryProxyUrl, monitor.acceptSuspended)
	// The ArgoCD API may return images in different orders between calls; sorting guarantees stable hash comparisons.
	normalizedImages := helpers.NormalizeImages(application.Status.Summary.Images)

	task.SavedAppStatus = models.SavedAppStatus{
		Status:     status,
		ImagesHash: helpers.GenerateHash(strings.Join(normalizedImages, ",")),
	}

	return nil
}

// WaitRollout polls the application status until it reaches a final state or times out.
//
// The timeout is enforced as a wall-clock deadline, not merely as a fixed number of poll
// attempts: a context deadline bounds the whole loop (and, through FetchApplication, each
// individual API call). This prevents the deployment from running far past its configured
// timeout when ArgoCD responds slowly — the failure mode reported in issue #304.
//
// Before each poll it re-reads the task status from the shared state: if a newer
// deployment for the same app has marked this task "cancelled" (issue #353), it
// stops immediately so no further ArgoCD API calls are made. Because the check
// goes through the shared state, this works across replicas in an HA setup — the
// cancelling deployment may be handled by a different replica than this poller.
func (monitor *DeploymentMonitor) WaitRollout(task models.Task) (*models.Application, error) {
	// application holds the most recent successfully-fetched state. It is deliberately assigned only
	// inside the success branch so that a fetch aborted by the deadline (which returns a nil application)
	// cannot clobber the last-known-good status we want to report on timeout.
	var application *models.Application

	refresh := monitor.resolveRefresh(task)
	retryOptions, deadline := monitor.configureRetryOptions(task)

	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	retryOptions = append(retryOptions, retry.Context(ctx))

	slog.Debug("Waiting for rollout", "id", task.Id, "deadline", deadline)

	err := retry.Do(func() error {
		// Stop before hitting ArgoCD if a newer deployment superseded this task.
		// The check is per-iteration: a cancellation that lands mid-iteration is
		// caught on the next poll, and if this iteration reaches a final state
		// first, its terminal status wins over "cancelled". That last-writer race
		// is accepted (issue #353) to avoid a status-conditional write.
		if monitor.taskSuperseded(task.Id) {
			return retry.Unrecoverable(errTaskSuperseded)
		}

		app, fetchErr := monitor.FetchApplication(ctx, task.App, refresh)
		if fetchErr != nil {
			return handleApplicationFetchError(task, fetchErr)
		}
		application = app

		if app.IsFireAndForgetModeActive() {
			slog.Debug("Fire and forget mode is active, skipping checks...", "id", task.Id)
			return nil
		}

		return checkRolloutStatus(task, app, monitor.registryProxyUrl, monitor.acceptSuspended)
	}, retryOptions...)

	// Once the retry budget or the wall-clock deadline is exhausted while still polling, surface the
	// last successfully-fetched application instead of the internal sentinel or the context error, so
	// the caller can report the real rollout status (e.g. "not synced", "not healthy") to the user.
	// If no fetch ever succeeded (application is nil), the error is returned so the caller can classify
	// the underlying failure (e.g. connection refused -> aborted).
	if application != nil && (errors.Is(err, errForceRetry) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		err = nil
	}

	return application, err
}

// configureRetryOptions derives retry attempts by preferring per-task overrides, then monitor defaults, and finally a legacy retry window aligned with the current retry delay.
//
// Alongside the retry options it returns the wall-clock deadline for the whole polling loop,
// computed as attempts*delay. The attempt count still caps the number of polls, while the
// deadline caps the elapsed time so that slow API responses cannot stretch the loop past the
// intended timeout (see WaitRollout).
func (monitor *DeploymentMonitor) configureRetryOptions(task models.Task) ([]retry.Option, time.Duration) {
	retryOptions := append([]retry.Option{}, monitor.retryOptions...)

	delay := monitor.retryDelay
	if delay <= 0 {
		delay = ArgoSyncRetryDelay
	}

	retryOptions = append(retryOptions, retry.Delay(delay))

	delaySeconds := helpers.CeilDivDuration(delay, time.Second)

	defaultAttempts := monitor.defaultAttempts
	if defaultAttempts == 0 {
		// Legacy fallback: legacyRetryIntervals steps at ArgoSyncRetryDelay pace, scaled to the current delay.
		fallbackWindow := time.Duration(legacyRetryIntervals) * ArgoSyncRetryDelay
		defaultAttempts = helpers.SafeIntToUint(helpers.CeilDivDuration(fallbackWindow, delay))
	}

	if task.Timeout <= 0 {
		slog.Debug("Task timeout is non-positive, defaulting to a fixed attempt count", "attempts", defaultAttempts, "id", task.Id)
		return append(retryOptions, retry.Attempts(defaultAttempts)), helpers.MulDurationSaturating(defaultAttempts, delay)
	}

	attempts := helpers.SafeIntToUint(int64(task.Timeout)/delaySeconds + 1)

	slog.Debug("Overriding task timeout", "timeout_seconds", task.Timeout, "retry_delay", delay, "delay_step_seconds", delaySeconds, "attempts", attempts, "id", task.Id)

	return append(retryOptions, retry.Attempts(attempts)), helpers.MulDurationSaturating(attempts, delay)
}

// ProcessDeploymentResult determines if the deployment was successful and updates the appropriate status and metrics.
func (monitor *DeploymentMonitor) ProcessDeploymentResult(task *models.Task, application *models.Application) {
	status := application.GetRolloutStatus(task.ListImages(), monitor.registryProxyUrl, monitor.acceptSuspended)
	if application.IsFireAndForgetModeActive() {
		status = models.ArgoRolloutAppSuccess
	}

	if status == models.ArgoRolloutAppSuccess {
		monitor.handleDeploymentSuccess(task)
	} else {
		monitor.handleDeploymentFailure(task, status, application)
	}
}

// taskSuperseded reports whether the task has been marked cancelled in the shared
// state, i.e. a newer deployment for the same app has superseded it. A read error
// is treated as "not superseded" so a transient state hiccup does not abort an
// otherwise healthy rollout; the check runs again on the next poll.
func (monitor *DeploymentMonitor) taskSuperseded(id string) bool {
	current, err := monitor.argo.State.GetTask(id)
	if err != nil {
		slog.Warn("Could not read task status to check for supersession", "error", err, "id", id)
		return false
	}
	return current.Status == models.StatusCancelledMessage
}

// HandleArgoAPIFailure processes API errors and updates task status accordingly.
// task is taken by pointer so the resolved terminal status is reflected back to
// the caller, keeping the outgoing failure notification in sync with the stored
// status (mirroring handleDeploymentSuccess/handleDeploymentFailure).
func (monitor *DeploymentMonitor) HandleArgoAPIFailure(task *models.Task, err error) {
	monitor.argo.metrics.AddFailedDeployment(task.App)
	finalStatus := determineFailureStatus(*task, err)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	slog.Warn("Deployment not completed", "status", finalStatus, "reason", reason, "id", task.Id)

	if err := monitor.argo.State.SetTaskStatus(task.Id, finalStatus, reason); err != nil {
		slog.Error("Failed to change task status", "error", err, "id", task.Id)
	}
	task.Status = finalStatus
}

func (monitor *DeploymentMonitor) handleDeploymentSuccess(task *models.Task) {
	slog.Info("App is running on the expected version.", "id", task.Id)
	monitor.argo.metrics.ResetFailedDeployment(task.App)
	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""); err != nil {
		slog.Error("Failed to change task status", "error", err, "id", task.Id)
	}
	task.Status = models.StatusDeployedMessage
}

func (monitor *DeploymentMonitor) handleDeploymentFailure(task *models.Task, status string, application *models.Application) {
	slog.Warn("App deployment failed.", "id", task.Id)
	monitor.argo.metrics.AddFailedDeployment(task.App)
	tree := monitor.fetchResourceTree(task)
	reason := fmt.Sprintf(
		"Application deployment failed. Rollout status is %s\n\n%s",
		status,
		application.GetRolloutMessage(status, task.ListImages(), tree),
	)
	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusFailedMessage, reason); err != nil {
		slog.Error("Failed to change task status", "error", err, "id", task.Id)
	}
	task.Status = models.StatusFailedMessage
}

// resourceTreeTimeout bounds the best-effort resource-tree fetch on the failure path so
// enriching the failure reason can never block terminal status reporting for long.
const resourceTreeTimeout = 10 * time.Second

// fetchResourceTree best-effort fetches the application's live resource tree to enrich the
// failure reason with pod-level causes (ImagePullBackOff, CrashLoopBackOff). It is deliberately
// non-fatal: any error yields a nil tree and GetRolloutMessage falls back to the app's top-level
// resources, so a resource-tree hiccup never prevents the deployment from being marked failed.
func (monitor *DeploymentMonitor) fetchResourceTree(task *models.Task) *models.ApplicationTree {
	ctx, cancel := context.WithTimeout(context.Background(), resourceTreeTimeout)
	defer cancel()

	tree, err := monitor.argo.api.GetResourceTree(ctx, task.App)
	if err != nil {
		slog.Debug("Could not fetch resource tree for failure diagnostics", "error", err, "id", task.Id)
		return nil
	}
	return tree
}

// handleApplicationFetchError ensures we don't retry for not found errors
func handleApplicationFetchError(task models.Task, err error) error {
	if task.IsAppNotFoundError(err) {
		return retry.Unrecoverable(err)
	}
	slog.Debug("Failed fetching application status", "error", err, "id", task.Id)
	return err
}

// checkRolloutStatus checks if the application completed rollout successfully
func checkRolloutStatus(task models.Task, application *models.Application, registryProxyUrl string, acceptSuspended bool) error {
	status := application.GetRolloutStatus(task.ListImages(), registryProxyUrl, acceptSuspended)

	switch status {
	case models.ArgoRolloutAppDegraded:
		slog.Debug("Application is degraded", "id", task.Id)
		normalizedImages := helpers.NormalizeImages(application.Status.Summary.Images)
		hash := helpers.GenerateHash(strings.Join(normalizedImages, ","))
		if !bytes.Equal(task.SavedAppStatus.ImagesHash, hash) {
			return retry.Unrecoverable(errors.New("application has degraded"))
		}
	case models.ArgoRolloutAppSuccess:
		slog.Debug("Application rollout finished", "id", task.Id)
		return nil
	default:
		slog.Debug("Application status is not final", "status", status, "id", task.Id)
	}
	return errForceRetry
}

// determineFailureStatus converts API errors into appropriate status messages
func determineFailureStatus(task models.Task, err error) string {
	if task.IsAppNotFoundError(err) {
		return models.StatusAppNotFoundMessage
	}
	if isArgoUnavailable(err) {
		return models.StatusAborted
	}
	return models.StatusFailedMessage
}

// isArgoUnavailable reports whether err means ArgoCD (or a proxy in front of it)
// was unreachable, so the rollout is aborted rather than blamed on the app.
func isArgoUnavailable(err error) bool {
	// Transport failure (timeout, connection refused, DNS, TLS, reset): the HTTP
	// client returns these as *url.Error, which implements net.Error.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// Context cancellation surfaced bare by the retry loop, not wrapped in url.Error.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// ArgoCD responded with a server error: the app state is unknown.
	var apiErr *ArgoAPIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 500
}
