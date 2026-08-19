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
)

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
// (issue #353).
func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	updater.monitor.BeginTracking()
	defer updater.monitor.EndTracking()

	sendNotification(task, updater.notifier)

	// start bounds the deployment-duration metric and the "after waiting ..." clause of a failure
	// message: a monotonic in-process clock over the rollout work only. It is taken after the start
	// notification so a slow synchronous notifier does not inflate the measured duration, and
	// deliberately not derived from task.Created (whose stored unit differs across state backends).
	start := time.Now()

	application, err := updater.waitForApplicationDeployment(task)

	var imageErr *ImageNotPartOfAppError

	switch {
	case errors.As(err, &imageErr):
		updater.monitor.HandleImageNotPartOfApp(&task, imageErr)
	case errors.Is(err, errTaskSuperseded):
		// A newer deployment for the same app already marked this task "cancelled"
		// in the shared state (possibly on another replica). Stop without writing a
		// status so we do not overwrite it; reflect it locally for the notification.
		slog.Info("Deployment superseded by a newer deployment for the same app; stopping.", "id", task.Id)
		task.Status = models.StatusCancelledMessage
	case err != nil:
		updater.monitor.HandleArgoAPIFailure(&task, err)
	default:
		updater.monitor.ProcessDeploymentResult(&task, application, time.Since(start))
	}

	// Only the deployed state is timed: a failure/abort/supersession is not a completed
	// deployment and its wall-clock is dominated by the timeout, so it would distort
	// the histogram.
	if task.Status == models.StatusDeployedMessage {
		updater.monitor.ObserveDeploymentDuration(task.App, time.Since(start).Seconds())
	}

	sendNotification(task, updater.notifier)
}

func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task) (*models.Application, error) {
	if updater.monitor.taskSuperseded(task.Id) {
		return nil, errTaskSuperseded
	}

	// The initial fetch happens before the timed polling loop, so it is bounded only by
	// the HTTP client's per-request timeout rather than the rollout deadline.
	app, err := updater.monitor.FetchApplication(context.Background(), task.App, updater.monitor.resolveRefresh(task))
	if err != nil {
		return nil, err
	}

	if err := updater.monitor.StoreInitialAppStatus(&task, app); err != nil {
		return nil, err
	}

	// The supersede predicate is re-checked inside the write-back retry loop so a
	// task that keeps retrying under
	// contention aborts (rather than overwriting a newer deployment) the moment a
	// newer one supersedes it.
	if err := updater.gitUpdater.UpdateIfNeeded(app, task, func() bool {
		return updater.monitor.taskSuperseded(task.Id)
	}); err != nil {
		if errors.Is(err, ErrDeploymentSuperseded) {
			return nil, errTaskSuperseded
		}
		return nil, err
	}

	return updater.monitor.WaitRollout(task)
}

func sendNotification(task models.Task, notifier *notifications.Notifier) {
	if notifier == nil {
		return
	}

	if err := notifier.Send(task); err != nil {
		slog.Error("Failed to dispatch notification", "error", err, "id", task.Id)
	}
}
