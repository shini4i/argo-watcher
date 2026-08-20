package argocd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/avast/retry-go/v4"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/notifications"
	"github.com/shini4i/argo-watcher/internal/state"
)

// ArgoStatusUpdater handles the monitoring and updating of ArgoCD application deployments
type ArgoStatusUpdater struct {
	monitor    *DeploymentMonitor
	gitUpdater *GitUpdater
	notifier   *notifications.Notifier
	// leaseRenewInterval and leaseTTL parameterize the claim a monitored rollout
	// holds. They are fields rather than constants so tests can drive a takeover
	// without waiting out a real lease.
	leaseRenewInterval time.Duration
	leaseTTL           time.Duration
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
	// BatchWriteBack enables the contention-coalescing batch write-back mode.
	BatchWriteBack bool
	// BatchMaxSize bounds the number of apps committed in a single batch flush.
	BatchMaxSize uint
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

	updater.leaseRenewInterval = state.TaskLeaseRenewInterval
	updater.leaseTTL = state.TaskLeaseTTL

	updater.monitor = NewDeploymentMonitor(argo, cfg.RegistryProxyURL, retryOptions, cfg.AcceptSuspended, cfg.RetryDelay)
	updater.monitor.defaultAttempts = cfg.RetryAttempts
	updater.monitor.refreshApp = cfg.RefreshApp

	var batcher *Batcher
	if cfg.BatchWriteBack {
		batcher = NewBatcher(cfg.Locker, cfg.RepoCachePath, cfg.BatchMaxSize, argo.metrics)
		slog.Info("Git write-back batch mode enabled", "max_batch_size", cfg.BatchMaxSize)
	}
	updater.gitUpdater = NewGitUpdater(cfg.Locker, cfg.RepoCachePath, argo.metrics, batcher)

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

// Close releases resources held by the updater, draining any in-flight batch
// write-backs (bounded by ctx) so a graceful shutdown does not abandon queued
// commits nor overrun its deadline.
func (updater *ArgoStatusUpdater) Close(ctx context.Context) {
	if updater.gitUpdater != nil {
		updater.gitUpdater.Close(ctx)
	}
}

// WaitForRollout monitors the application until it reaches a final state (deployed
// or failed), or stops early if a newer deployment for the same app supersedes it
// (issue #353), or if another replica takes the task over.
//
// resumed marks a task picked up from another replica: its start notification was
// already sent by the replica that accepted it, so sending a second one would
// announce the same deployment twice.
func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task, resumed bool) {
	updater.waitForRollout(task, resumed, neverDraining)
}

// neverDraining is the abandon predicate for a rollout monitored by the replica
// that accepted the deployment. Such a task has nowhere to be handed back to:
// nothing else holds its claim, so it is watched until it finishes or until the
// process ends with it.
func neverDraining() bool { return false }

// waitForRollout is WaitForRollout with the condition under which this replica
// gives the rollout up: draining reports that shutdown has begun, which ends the
// monitoring as a takeover would — without a status, so the replica that resumes
// the task records the outcome instead.
func (updater *ArgoStatusUpdater) waitForRollout(task models.Task, resumed bool, draining func() bool) {
	updater.monitor.BeginTracking()
	defer updater.monitor.EndTracking()

	// Renewing the claim for as long as this replica polls the rollout is what keeps
	// a sweep on another replica from taking over a deployment that is being watched.
	lease := newLeaseGuard(updater.monitor.argo.State, task.Id, updater.leaseRenewInterval, updater.leaseTTL)
	defer lease.Stop()

	if !resumed {
		sendNotification(task, updater.notifier)
	}

	// start bounds the deployment-duration metric: a monotonic in-process clock over the whole
	// deployment, write-back included. It is taken after the start notification so a slow
	// synchronous notifier does not inflate the measured duration, and deliberately not derived
	// from task.Created (whose stored unit differs across state backends). The failure message
	// reports waited instead, which covers the rollout polling alone.
	start := time.Now()

	// Both conditions end the rollout the same way, so the poll loop and the
	// write-back are given one predicate: stop at the next boundary, without a
	// status. Composing them here rather than inside the lease guard keeps the guard
	// about the claim alone.
	abandoned := func() bool { return lease.Lost() || draining() }

	application, waited, confirmed, err := updater.waitForApplicationDeployment(task, resumed, abandoned)

	// Re-checked here because only the poll loop and the write-back consult the
	// predicate themselves. Every other way out — a failed fetch, a write-back error,
	// or an ordinary success — would otherwise write a status and notify on a task
	// this replica lost during the last poll interval, or no longer finishes.
	switch {
	case lease.Lost():
		// Whatever the rollout ended as, the replica that took the claim is monitoring
		// it too and reaches the same outcome — a supersession included. Reporting it
		// here as well would announce one deployment twice.
		err = errLeaseLost
	case errors.Is(err, errTaskSuperseded):
		// Left as it is even while draining: the cancellation is already persisted by
		// the deployment that caused it, and a cancelled task is never re-claimed by a
		// sweep — so this replica is the last one able to announce it.
	case draining():
		err = errReplicaDraining
	}

	var imageErr *ImageNotPartOfAppError

	switch {
	case errors.Is(err, errReplicaDraining):
		// This replica finishes nothing from here on: its claims are released in the
		// last shutdown phase and another replica resumes the deployment. A status
		// written now would be decided by a monitor that is about to disappear — and
		// once the write-back batcher is closed, it would be a failure for a rollout
		// that is otherwise healthy.
		slog.Info("Stopped monitoring a deployment while shutting down; another replica will resume it.", "id", task.Id)
		return
	case errors.Is(err, errLeaseLost):
		// Another replica owns this task now and is monitoring the same rollout.
		// Writing a status here would clobber the outcome it is about to record, and
		// notifying would announce a result this replica no longer decides.
		slog.Info("Stopped monitoring a deployment taken over by another replica.", "id", task.Id)
		return
	case errors.As(err, &imageErr):
		updater.monitor.HandleImageNotPartOfApp(&task, imageErr)
	case errors.Is(err, errTaskSuperseded):
		// A newer deployment for the same app already marked this task "cancelled"
		// in the shared state (possibly on another replica). Stop without writing a
		// status so we do not overwrite it; reflect it locally for the notification.
		slog.Info("Deployment superseded by a newer deployment for the same app; stopping.", "id", task.Id)
		task.Status = models.StatusCancelledMessage
	case err != nil:
		updater.monitor.HandleArgoAPIFailure(&task, err, confirmed)
	default:
		updater.monitor.ProcessDeploymentResult(&task, application, waited)
	}

	// Only the deployed state is timed: a failure/abort/supersession is not a completed
	// deployment and its wall-clock is dominated by the timeout, so it would distort
	// the histogram.
	if task.Status == models.StatusDeployedMessage {
		updater.monitor.ObserveDeploymentDuration(task.App, time.Since(start).Seconds())
	}

	sendNotification(task, updater.notifier)
}

// abortedWriteBackCause names why a write-back gave up. Both of its stop conditions
// surface as ErrDeploymentSuperseded, so the shared state decides which one is
// reported: a task cancelled there is reported as superseded even when this replica
// is also giving the rollout up, because a cancelled task is never re-claimed by a
// sweep and no successor could announce it.
func (updater *ArgoStatusUpdater) abortedWriteBackCause(taskId string, abandoned bool) error {
	if abandoned && !updater.monitor.taskSuperseded(taskId) {
		return errLeaseLost
	}

	return errTaskSuperseded
}

// waitForApplicationDeployment fetches the application, writes the image tag back when it is
// managed, and polls the rollout. The returned bool reports whether ArgoCD confirmed the
// application: every metric carrying the app name waits for that (issue #552).
func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task, resumed bool, abandoned func() bool) (*models.Application, time.Duration, bool, error) {
	if updater.monitor.taskSuperseded(task.Id) {
		return nil, 0, false, errTaskSuperseded
	}

	// The initial fetch happens before the timed polling loop, so it is bounded only by
	// the HTTP client's per-request timeout rather than the rollout deadline.
	app, err := updater.monitor.ConfirmApplication(context.Background(), task.App, updater.monitor.resolveRefresh(task))
	if err != nil {
		return nil, 0, false, err
	}

	// ArgoCD answered for the application, so the name is no longer merely what the
	// submission claimed and may label the deployment. Only the replica that accepted the
	// deployment counts it: a handover monitors the same deployment over again, and
	// counting it per replica would inflate the deployment count of a rolled replica's apps.
	if !resumed {
		updater.monitor.CountProcessedDeployment(task.App)
	}

	if err := updater.monitor.StoreInitialAppStatus(&task, app); err != nil {
		return nil, 0, true, err
	}

	// The stop predicate is re-checked inside the write-back retry loop so a task
	// that keeps retrying under contention aborts the moment a newer deployment
	// supersedes it — rather than overwriting that deployment — or the moment this
	// replica gives the rollout up, which would otherwise have two replicas pushing
	// the same write-back, or push after the batcher was drained.
	if err := updater.gitUpdater.UpdateIfNeeded(app, task, func() bool {
		return updater.monitor.taskSuperseded(task.Id) || abandoned()
	}); err != nil {
		if errors.Is(err, ErrDeploymentSuperseded) {
			return nil, 0, true, updater.abortedWriteBackCause(task.Id, abandoned())
		}
		return nil, 0, true, err
	}

	application, waited, err := updater.monitor.WaitRollout(task, abandoned)
	return application, waited, true, err
}

func sendNotification(task models.Task, notifier *notifications.Notifier) {
	if notifier == nil {
		return
	}

	if err := notifier.Send(task); err != nil {
		slog.Error("Failed to dispatch notification", "error", err, "id", task.Id)
	}
}
