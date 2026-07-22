package argocd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/state"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	// ArgoSyncRetryDelay is the delay between ArgoCD sync status retries.
	ArgoSyncRetryDelay = 15 * time.Second
	// ArgoLivenessProbeInterval is how often the background liveness probe
	// refreshes the argocd_unavailable metric (see Argo.StartLivenessProbe).
	ArgoLivenessProbeInterval = 30 * time.Second
)

// rollbackHistoryWindow bounds how many of an app's most recent successfully
// deployed tasks are inspected when deciding whether a new deployment is a
// rollback. Rollbacks to a version older than this window are not flagged; this
// is an accepted simplification to keep the per-deployment lookup cheap.
const rollbackHistoryWindow = 100

// Unavailability reasons reported by Check and UnavailableReason. They identify
// which subsystem is unreachable so the frontend banner can name the exact cause
// (ArgoCD vs the state backend) instead of hedging with "one or the other". An
// empty reason means everything is reachable.
const (
	ReasonNone     = ""         // Everything reachable.
	ReasonArgoCD   = "argocd"   // ArgoCD API is unreachable or login failed.
	ReasonDatabase = "database" // The state backend (database) is unreachable.
	ReasonBoth     = "both"     // ArgoCD and the state backend are both unreachable.
)

const (
	// ArgoAPIErrorTemplate is the template for ArgoCD API errors.
	ArgoAPIErrorTemplate = "ArgoCD API Error: %s"
	// supersededTaskReason is the status reason stored on a deployment that was
	// cancelled because a newer deployment of the same image superseded it.
	supersededTaskReason = "superseded by a newer deployment for the same image"
)

// Argo is the primary controller for watcher operations.
type Argo struct {
	metrics prometheus.MetricsInterface
	api     ArgoApiInterface
	State   state.TaskRepository
	// reason caches which subsystem, if any, was unreachable at the most recent
	// Check so it can be read synchronously (IsAvailable / UnavailableReason) off
	// any request path. An empty reason (ReasonNone) means everything is
	// reachable. The background liveness probe keeps it fresh; AddTask gates on it
	// and the frontend "unreachable" banner names the cause from it (issue #498).
	// It is a pointer so the value copies of Argo held by the updater and
	// deployment monitor (which never read it) stay freely copyable; Init
	// allocates it. The stored value is always a string, so Load can assert it.
	reason *atomic.Value
}

// Init initializes the Argo controller with its dependencies.
func (argo *Argo) Init(state state.TaskRepository, api ArgoApiInterface, metrics prometheus.MetricsInterface) {
	argo.api = api
	argo.State = state
	argo.metrics = metrics
	// Assume ArgoCD is reachable until the first Check proves otherwise. Starting
	// unavailable instead was considered and rejected: it would flash the
	// "unreachable" banner and reject deploys on every healthy startup (crying
	// wolf), for the sub-second until the first probe runs. This optimistic
	// default only briefly mis-reports during a genuine startup-time outage,
	// which the liveness probe corrects within ~one probe cycle.
	argo.reason = &atomic.Value{}
	argo.reason.Store(ReasonNone)
}

// Check performs a health check on ArgoCD and the state backend. Both subsystems
// are evaluated independently so a simultaneous outage is reported as
// ReasonBoth rather than letting the database check mask an ArgoCD problem.
func (argo *Argo) Check() (string, error) {
	databaseUp := argo.State.Check()
	userLoggedIn, loginError := argo.api.GetUserInfo()

	// Resolve the ArgoCD side to a specific error (nil when reachable) before
	// combining it with the database result.
	var argoErr error
	switch {
	case loginError != nil:
		argoErr = errors.New(models.StatusArgoCDUnavailableMessage)
	case userLoggedIn == nil || !userLoggedIn.LoggedIn:
		argoErr = errors.New(models.StatusArgoCDFailedLogin)
	}

	databaseDown := !databaseUp
	argocdDown := argoErr != nil

	switch {
	case databaseDown && argocdDown:
		argo.setReason(ReasonBoth)
		return "down", fmt.Errorf("%s; %w", models.StatusConnectionUnavailable, argoErr)
	case databaseDown:
		argo.setReason(ReasonDatabase)
		return "down", errors.New(models.StatusConnectionUnavailable)
	case argocdDown:
		argo.setReason(ReasonArgoCD)
		return "down", argoErr
	default:
		argo.setReason(ReasonNone)
		return "up", nil
	}
}

// setReason records the latest unavailability cause in one place, keeping the
// synchronously-readable cache (IsAvailable / UnavailableReason) and the two
// per-subsystem gauges in lockstep. The gauges are independent: a state-backend
// outage must NOT raise argocd_unavailable, and vice versa, so a ReasonBoth
// raises both while a single-cause reason raises only its own.
func (argo *Argo) setReason(reason string) {
	argo.reason.Store(reason)
	argo.metrics.SetArgoUnavailable(reason == ReasonArgoCD || reason == ReasonBoth)
	argo.metrics.SetStateUnavailable(reason == ReasonDatabase || reason == ReasonBoth)
}

// IsAvailable reports whether everything was reachable at the most recent Check,
// without performing a live probe. The background liveness probe
// (StartLivenessProbe) keeps this current, so reads never block on a live call.
func (argo *Argo) IsAvailable() bool {
	return argo.UnavailableReason() == ReasonNone
}

// UnavailableReason reports which subsystem was unreachable at the most recent
// Check (ReasonArgoCD or ReasonDatabase), or ReasonNone when everything was
// reachable. It lets the frontend banner name the exact cause.
func (argo *Argo) UnavailableReason() string {
	return argo.reason.Load().(string)
}

// StartLivenessProbe periodically runs Check() so the argocd_unavailable metric
// keeps reflecting ArgoCD reachability even during read-only periods (no new
// deployments). Check() refreshes that gauge as a side effect; the probe's
// return value is intentionally discarded. This is the single ambient refresher:
// it lives here, off every request path, so listing tasks never blocks on a
// live ArgoCD call (see GetTasks). It runs until ctx is cancelled and is meant
// to be launched in its own goroutine.
func (argo *Argo) StartLivenessProbe(ctx context.Context, interval time.Duration) {
	// Probe once immediately so the gauge is populated at startup instead of
	// only after the first interval elapses. Check() updates the metric itself;
	// log at debug so an outage leaves a correlatable trace without spamming
	// logs every interval.
	if _, err := argo.Check(); err != nil {
		slog.Debug("ArgoCD liveness probe failed", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := argo.Check(); err != nil {
				slog.Debug("ArgoCD liveness probe failed", "error", err)
			}
		}
	}
}

// AddTask validates a new deployment task and adds it to the task repository.
func (argo *Argo) AddTask(task models.Task) (*models.Task, error) {
	// Gate on the cached reachability instead of a live Check(): a deploy
	// attempted during an ArgoCD outage then fails fast with a clear error
	// rather than blocking on the full API retry budget (ARGO_API_RETRIES ×
	// ARGO_API_TIMEOUT) until the client's own HTTP timeout fires and masks the
	// cause as a bare "context deadline exceeded" (issue #498). The liveness
	// probe keeps this state fresh.
	if !argo.IsAvailable() {
		return nil, errors.New(models.StatusArgoCDUnavailableMessage)
	}

	if task.Images == nil || len(task.Images) == 0 {
		return nil, fmt.Errorf("trying to create task without images")
	}

	if task.App == "" {
		return nil, fmt.Errorf("trying to create task without app name")
	}

	// Always overwrite the rollback fields from server-side history so a
	// client-supplied value (e.g. echoed back by the "rollback to this version"
	// action) can never influence the stored result.
	task.RollbackTargetId = argo.detectRollback(task)
	task.IsRollback = task.RollbackTargetId != ""

	// Supersede any in-flight deployment for this app that targets one of the same
	// images before starting a new one, so the watcher stops polling ArgoCD for a
	// rollout nobody is waiting on anymore (issue #353). Matching on image name
	// (not just the app) keeps independent per-image deployments of the same app
	// from cancelling each other. This runs against the shared state, so in an HA
	// setup it also cancels rollouts being watched by other replicas. Best-effort:
	// a failure here must not block the new deployment.
	if cancelled, err := argo.State.CancelInProgressTasks(task.App, task.Images, supersededTaskReason); err != nil {
		slog.Warn("Failed to cancel in-progress deployments for the app", "error", err, "app", task.App)
	} else if cancelled > 0 {
		slog.Info("Cancelled in-progress deployment(s) superseded by the new task", "cancelled", cancelled, "app", task.App)
	}

	newTask, err := argo.State.AddTask(task)
	if err != nil {
		return nil, err
	}

	slog.Info("A new task was triggered", "id", newTask.Id)
	for index, value := range newTask.Images {
		slog.Info("Task image expecting tag", "index", index, "tag", value.Tag, "app", task.App, "id", newTask.Id)
	}

	argo.metrics.AddProcessedDeployment(task.App)
	return newTask, nil
}

// detectRollback reports whether deploying task represents a rollback and, if
// so, returns the ID of the task it rolls back to. A rollback is a deployment
// whose image set was successfully deployed at some earlier point for the app
// AND differs from the current (most recently deployed) version; redeploying
// the current version is not a rollback. The returned ID is the most recent
// earlier task carrying that image set. An empty string means "not a rollback".
// The lookup is bounded by rollbackHistoryWindow.
func (argo *Argo) detectRollback(task models.Task) string {
	deployed, _ := argo.State.GetTasks(0, float64(time.Now().Unix()), task.App, models.StatusDeployedMessage, rollbackHistoryWindow, 0)
	if len(deployed) == 0 {
		return ""
	}

	target := imageSignature(task)

	// GetTasks orders by created DESC, so deployed[0] is the current version.
	// Matching it means we are redeploying the current version, not rolling back.
	if imageSignature(deployed[0]) == target {
		return ""
	}

	// deployed[1:] is scanned newest-first, so the first match is the most
	// recent earlier deployment of the target image set.
	for _, previous := range deployed[1:] {
		if imageSignature(previous) == target {
			return previous.Id
		}
	}

	return ""
}

// imageSignature returns a stable, order-independent key for a task's image set
// so two deployments of the same images compare equal regardless of ordering.
func imageSignature(task models.Task) string {
	return strings.Join(helpers.NormalizeImages(task.ListImages()), ",")
}

// GetTasks retrieves tasks from the state within a given time range and optional app/status filters and pagination window.
//
// Listing is a pure read from the state store and is deliberately NOT gated on
// ArgoCD reachability. Stored task history must stay viewable even when ArgoCD
// is unavailable (e.g. a DNS/network outage): coupling the read to a live
// ArgoCD `session/userinfo` call would otherwise make the whole list hang on the
// API retry budget and then hide existing tasks behind an error. Write paths
// (AddTask) no longer run a live Check() either: they gate on the cached
// reachability (IsAvailable) so a deploy fails fast during an outage. The
// background liveness probe (StartLivenessProbe) is therefore the single place
// the argocd_unavailable metric and the cached state are refreshed. Note the
// /healthz endpoint probes only the state backend (SimpleHealthCheck), not
// ArgoCD.
func (argo *Argo) GetTasks(startTime float64, endTime float64, app string, status string, limit int, offset int) models.TasksResponse {
	tasks, total := argo.State.GetTasks(startTime, endTime, app, status, limit, offset)

	return models.TasksResponse{
		Tasks: tasks,
		Total: total,
	}
}

// SimpleHealthCheck checks only the state backend connection.
func (argo *Argo) SimpleHealthCheck() bool {
	return argo.State.Check()
}
