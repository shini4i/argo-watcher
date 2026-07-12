package argocd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/notifications"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/pkg/updater"
)

const (
	failedToUpdateTaskStatusTemplate = "Failed to change task status: %s"
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

// GitUpdater encapsulates the logic required to update Git repositories watched by ArgoCD.
type GitUpdater struct {
	locker        lock.Locker
	repoCachePath string
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

	log.Debug().Str("id", task.Id).Dur("deadline", deadline).Msg("Waiting for rollout")

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

		// Early return for fire and forget mode
		if app.IsFireAndForgetModeActive() {
			log.Debug().Str("id", task.Id).Msg("Fire and forget mode is active, skipping checks...")
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

// mulDurationSaturating returns count*unit, clamped to math.MaxInt64 to avoid the uint->int64
// overflow that a very large (client-supplied) attempt count could otherwise wrap into a negative
// duration. A non-positive unit or zero count yields 0.
func mulDurationSaturating(count uint, unit time.Duration) time.Duration {
	if unit <= 0 || count == 0 {
		return 0
	}
	// unit > 0 is guaranteed above, so the quotient is a non-negative int64 that fits in uint64.
	maxCount := uint64(math.MaxInt64 / int64(unit)) // #nosec G115 -- non-negative quotient
	if uint64(count) > maxCount {
		return time.Duration(math.MaxInt64)
	}
	// The guard above proves count fits in a positive int64, so this conversion cannot overflow.
	return time.Duration(count) * unit // #nosec G115 -- count bounded by check above
}

// ceilDivDuration returns the ceiling of d/unit as an int64, with a minimum of 1.
// A non-positive unit is treated as invalid and returns 1 to avoid division by zero.
func ceilDivDuration(d, unit time.Duration) int64 {
	if unit <= 0 {
		return 1
	}
	result := int64((d + unit - 1) / unit)
	if result <= 0 {
		return 1
	}
	return result
}

// safeIntToUint converts an int64 to uint with overflow protection, enforcing a minimum of 1.
// On 32-bit platforms where uint is 32 bits, values exceeding math.MaxUint32 are clamped to max uint.
func safeIntToUint(v int64) uint {
	if v <= 0 {
		return 1
	}
	maxUint := ^uint(0)
	if uint64(v) > uint64(maxUint) {
		return maxUint
	}
	return uint(v) // #nosec G115 -- overflow checked above
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

	delaySeconds := ceilDivDuration(delay, time.Second)

	defaultAttempts := monitor.defaultAttempts
	if defaultAttempts == 0 {
		// Legacy fallback: legacyRetryIntervals steps at ArgoSyncRetryDelay pace, scaled to the current delay.
		fallbackWindow := time.Duration(legacyRetryIntervals) * ArgoSyncRetryDelay
		defaultAttempts = safeIntToUint(ceilDivDuration(fallbackWindow, delay))
	}

	if task.Timeout <= 0 {
		log.Debug().Str("id", task.Id).Msgf("Task timeout is non-positive, defaulting to %d attempts", defaultAttempts)
		return append(retryOptions, retry.Attempts(defaultAttempts)), mulDurationSaturating(defaultAttempts, delay)
	}

	attempts := safeIntToUint(int64(task.Timeout)/delaySeconds + 1)

	log.Debug().Str("id", task.Id).Msgf(
		"Overriding task timeout to %ds with retry delay %s (~%d second step, %d attempts)",
		task.Timeout,
		delay,
		delaySeconds,
		attempts,
	)

	return append(retryOptions, retry.Attempts(attempts)), mulDurationSaturating(attempts, delay)
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
		log.Warn().Str("id", id).Msgf("Could not read task status to check for supersession: %s", err)
		return false
	}
	return current.Status == models.StatusCancelledMessage
}

// HandleArgoAPIFailure processes API errors and updates task status accordingly.
func (monitor *DeploymentMonitor) HandleArgoAPIFailure(task models.Task, err error) {
	monitor.argo.metrics.AddFailedDeployment(task.App)
	finalStatus := determineFailureStatus(task, err)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	log.Warn().Str("id", task.Id).Msgf("Deployment failed with status \"%s\". Aborting with error: %s", finalStatus, reason)

	if err := monitor.argo.State.SetTaskStatus(task.Id, finalStatus, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
}

func (monitor *DeploymentMonitor) handleDeploymentSuccess(task *models.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the expected version.")
	monitor.argo.metrics.ResetFailedDeployment(task.App)
	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusDeployedMessage
}

func (monitor *DeploymentMonitor) handleDeploymentFailure(task *models.Task, status string, application *models.Application) {
	log.Info().Str("id", task.Id).Msg("App deployment failed.")
	monitor.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf(
		"Application deployment failed. Rollout status is %s\n\n%s",
		status,
		application.GetRolloutMessage(status, task.ListImages()),
	)
	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusFailedMessage, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusFailedMessage
}

// NewGitUpdater creates a GitUpdater instance.
func NewGitUpdater(locker lock.Locker, repoCachePath string) *GitUpdater {
	return &GitUpdater{
		locker:        locker,
		repoCachePath: repoCachePath,
	}
}

// UpdateIfNeeded updates the git repository if the application is managed by the
// watcher and has valid credentials. isSuperseded is an optional (at most one)
// predicate forwarded to the write-back retry loop so a task superseded by a
// newer deployment aborts instead of committing a stale image tag.
func (gitUpdater *GitUpdater) UpdateIfNeeded(app *models.Application, task models.Task, isSuperseded ...func() bool) error {
	if !app.IsManagedByWatcher() || !task.Validated {
		log.Debug().Str("id", task.Id).Msg("Skipping git repo update: application not managed by watcher or task not validated.")
		return nil
	}

	gitopsRepo, err := models.NewGitopsRepo(app, gitUpdater.repoCachePath)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to get gitops repo info for app %s: %s", task.App, err)
		return err
	}

	gitUpdateFunc := func() error {
		log.Debug().Str("id", task.Id).Msg("Application managed by watcher. Initiating git repo update.")
		return gitUpdater.updateGitRepo(app, &task, &gitopsRepo, isSuperseded...)
	}

	err = gitUpdater.locker.WithLock(gitopsRepo.RepoUrl, gitUpdateFunc)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed git repo update for app %s: %s", task.App, err)
		return err
	}

	return nil
}

func (gitUpdater *GitUpdater) updateGitRepo(app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo, isSuperseded ...func() bool) error {
	// context.Background() is intentional: the call stack above this point
	// does not carry a context. Propagating a cancellable context from
	// WaitForRollout is a future improvement.
	err := app.UpdateGitImageTag(context.Background(), task, gitopsRepo, updater.GitClient{}, isSuperseded...)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to update git repo. Error: %s", err.Error())
		return err
	}
	return nil
}

// ArgoStatusUpdater handles the monitoring and updating of ArgoCD application deployments
type ArgoStatusUpdater struct {
	monitor    *DeploymentMonitor
	gitUpdater *GitUpdater
	notifier   *notifications.Notifier
}

// ArgoStatusUpdaterConfig groups the dependencies required to bootstrap an ArgoStatusUpdater.
type ArgoStatusUpdaterConfig struct {
	RetryAttempts    uint
	RetryDelay       time.Duration
	RegistryProxyURL string
	RepoCachePath    string
	AcceptSuspended  bool
	RefreshApp       bool
	WebhookConfig    *config.WebhookConfig
	MattermostConfig *config.MattermostConfig
	Locker           lock.Locker
}

// Init initializes the ArgoStatusUpdater with the provided configuration
func (updater *ArgoStatusUpdater) Init(argo Argo, cfg ArgoStatusUpdaterConfig) error {
	retryOptions := []retry.Option{
		retry.DelayType(retry.FixedDelay),
		retry.LastErrorOnly(true),
	}

	if cfg.Locker == nil {
		return fmt.Errorf("locker cannot be nil")
	}

	updater.monitor = NewDeploymentMonitor(argo, cfg.RegistryProxyURL, retryOptions, cfg.AcceptSuspended, cfg.RetryDelay)
	updater.monitor.defaultAttempts = cfg.RetryAttempts
	updater.monitor.refreshApp = cfg.RefreshApp
	updater.gitUpdater = NewGitUpdater(cfg.Locker, cfg.RepoCachePath)

	var strategies []notifications.NotificationStrategy

	httpClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	if cfg.WebhookConfig != nil && cfg.WebhookConfig.Enabled {
		webhookStrategy, err := notifications.NewWebhookStrategy(cfg.WebhookConfig, httpClient)
		if err != nil {
			return err
		}

		strategies = append(strategies, webhookStrategy)
	}

	if cfg.MattermostConfig != nil && cfg.MattermostConfig.Enabled {
		mattermostStrategy, err := notifications.NewMattermostStrategy(cfg.MattermostConfig, httpClient)
		if err != nil {
			return err
		}

		strategies = append(strategies, mattermostStrategy)
	}

	if len(strategies) == 0 {
		return nil
	}

	updater.notifier = notifications.NewNotifier(strategies...)
	return nil
}

// WaitForRollout is the main entry point for tracking an application deployment
// It monitors the application until it reaches a final state (deployed or failed),
// or stops early if a newer deployment for the same app supersedes it (issue #353).
func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	updater.monitor.BeginTracking()
	defer updater.monitor.EndTracking()

	// notify about the deployment start
	sendNotification(task, updater.notifier)

	// wait for application to get into deployed status or timeout
	application, err := updater.waitForApplicationDeployment(task)

	switch {
	case errors.Is(err, errTaskSuperseded):
		// A newer deployment for the same app already marked this task "cancelled"
		// in the shared state (possibly on another replica). Stop without writing a
		// status so we do not overwrite it; reflect it locally for the notification.
		log.Info().Str("id", task.Id).Msg("Deployment superseded by a newer deployment for the same app; stopping.")
		task.Status = models.StatusCancelledMessage
	case err != nil:
		// handle application failure
		updater.monitor.HandleArgoAPIFailure(task, err)
	default:
		// process deployment result
		updater.monitor.ProcessDeploymentResult(&task, application)
	}

	// send webhook event about the deployment result
	sendNotification(task, updater.notifier)
}

// waitForApplicationDeployment coordinates the deployment monitoring process
// It checks initial status, updates the git repo if needed, and waits for rollout.
func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task) (*models.Application, error) {
	// Bail out before any ArgoCD call or git update if the task was already
	// superseded by a newer deployment for the same app.
	if updater.monitor.taskSuperseded(task.Id) {
		return nil, errTaskSuperseded
	}

	// Fetch initial app state. This single call happens before the timed polling loop, so it is
	// bounded only by the HTTP client's per-request timeout rather than the rollout deadline.
	app, err := updater.monitor.FetchApplication(context.Background(), task.App, updater.monitor.resolveRefresh(task))
	if err != nil {
		return nil, err
	}

	// Save the initial application status
	if err := updater.monitor.StoreInitialAppStatus(&task, app); err != nil {
		return nil, err
	}

	// Handle git repo update if needed. The supersede predicate is re-checked
	// inside the write-back retry loop so a task that keeps retrying under
	// contention aborts (rather than overwriting a newer deployment) the moment a
	// newer one supersedes it.
	if err := updater.gitUpdater.UpdateIfNeeded(app, task, func() bool {
		return updater.monitor.taskSuperseded(task.Id)
	}); err != nil {
		if errors.Is(err, models.ErrDeploymentSuperseded) {
			return nil, errTaskSuperseded
		}
		return nil, err
	}

	// Wait for rollout completion
	return updater.monitor.WaitRollout(task)
}

// handleApplicationFetchError ensures we don't retry for not found errors
func handleApplicationFetchError(task models.Task, err error) error {
	if task.IsAppNotFoundError(err) {
		return retry.Unrecoverable(err)
	}
	log.Debug().Str("id", task.Id).Msgf("Failed fetching application status. Error: %s", err.Error())
	return err
}

// checkRolloutStatus checks if the application completed rollout successfully
func checkRolloutStatus(task models.Task, application *models.Application, registryProxyUrl string, acceptSuspended bool) error {
	status := application.GetRolloutStatus(task.ListImages(), registryProxyUrl, acceptSuspended)

	switch status {
	case models.ArgoRolloutAppDegraded:
		log.Debug().Str("id", task.Id).Msgf("Application is degraded")
		normalizedImages := helpers.NormalizeImages(application.Status.Summary.Images)
		hash := helpers.GenerateHash(strings.Join(normalizedImages, ","))
		if !bytes.Equal(task.SavedAppStatus.ImagesHash, hash) {
			return retry.Unrecoverable(errors.New("application has degraded"))
		}
	case models.ArgoRolloutAppSuccess:
		log.Debug().Str("id", task.Id).Msgf("Application rollout finished")
		return nil
	default:
		log.Debug().Str("id", task.Id).Msgf("Application status is not final. Status received \"%s\"", status)
	}
	return errForceRetry
}

// determineFailureStatus converts API errors into appropriate status messages
func determineFailureStatus(task models.Task, err error) string {
	if task.IsAppNotFoundError(err) {
		return models.StatusAppNotFoundMessage
	}
	if strings.Contains(err.Error(), argoUnavailableErrorMessage) {
		return models.StatusAborted
	}
	return models.StatusFailedMessage
}

// sendNotification sends the task update through the configured notifier if available.
func sendNotification(task models.Task, notifier *notifications.Notifier) {
	if notifier == nil {
		return
	}

	if err := notifier.Send(task); err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to dispatch notification. Error: %s", err.Error())
	}
}
