package argocd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

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

// ErrTaskHistoryUnavailable marks a submission refused because the app's deployment
// history could not be read. The cause carries driver text and submission takes no
// credential, so the handler answers with a fixed message and leaves detail to the log.
var ErrTaskHistoryUnavailable = errors.New("could not read the deployment history")

// rollbackHistoryWindow bounds how many of an app's most recent successfully
// deployed tasks are inspected when deciding whether a new deployment is a
// rollback. Rollbacks to a version older than this window are not flagged; this
// is an accepted simplification to keep the per-deployment lookup cheap.
const rollbackHistoryWindow = 100

// Unavailability reasons reported by Check and UnavailableReason. They identify
// which subsystem is unreachable so the frontend banner can name the exact cause
// (ArgoCD vs the state backend) instead of hedging with "one or the other".
const (
	ReasonNone     = ""         // Everything reachable.
	ReasonArgoCD   = "argocd"   // ArgoCD API is unreachable or login failed.
	ReasonDatabase = "database" // The state backend (database) is unreachable.
	ReasonBoth     = "both"
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

// Init initializes the Argo controller with its dependencies. It allocates the
// reason cache, so it must run before any read of it.
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

// setReason keeps the synchronously-readable cache and the two per-subsystem gauges
// in lockstep. The gauges are independent: a state-backend outage must NOT raise
// argocd_unavailable, and vice versa.
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
// Check, or ReasonNone when everything was reachable.
func (argo *Argo) UnavailableReason() string {
	return argo.reason.Load().(string)
}

// StartLivenessProbe periodically runs Check() so the argocd_unavailable metric
// keeps reflecting ArgoCD reachability even during read-only periods (no new
// deployments). This is the single ambient refresher: it lives here, off every
// request path, so listing tasks never blocks on a live ArgoCD call (see GetTasks).
// It runs until ctx is cancelled and is meant to be launched in its own goroutine.
func (argo *Argo) StartLivenessProbe(ctx context.Context, interval time.Duration) {
	// Probe once immediately so the gauge is populated at startup instead of only
	// after the first interval elapses. Log at debug so an outage leaves a
	// correlatable trace without spamming logs every interval.
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

// AddTask validates a new deployment task, overwrites the fields a client must not
// set, and adds it to the task repository.
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

	if len(task.Images) == 0 {
		return nil, fmt.Errorf("trying to create task without images")
	}

	if task.App == "" {
		return nil, fmt.Errorf("trying to create task without app name")
	}

	// Always overwrite the rollback fields from server-side history so a
	// client-supplied value (e.g. echoed back by the "rollback to this version"
	// action) can never influence the stored result.
	rollbackTargetId, err := argo.detectRollback(task)
	if err != nil {
		return nil, err
	}
	task.RollbackTargetId = rollbackTargetId
	task.IsRollback = task.RollbackTargetId != ""

	// StatusReason and Updated are server-owned but JSON-bindable, and submission takes
	// no credential. Clearing them keeps a client-supplied value out of the in-memory
	// store and out of the task the start notification renders.
	task.StatusReason = ""
	task.Updated = 0

	// Superseding stops the watcher polling ArgoCD for a rollout nobody is waiting
	// on anymore (issue #353). It shares a step with the insert so two submissions
	// racing here cannot each miss the other's task and both survive.
	newTask, cancelled, err := argo.State.SupersedeAndAdd(task, supersededTaskReason)
	if err != nil {
		return nil, err
	}
	if cancelled > 0 {
		slog.Info("Cancelled in-progress deployment(s) superseded by the new task", "cancelled", cancelled, "app", task.App)
	}

	// This replica is about to monitor the rollout, so it claims the task before any
	// sweep can offer it elsewhere. Best-effort: a claim that cannot be written is a
	// database blip, and the sweep leaves rows this young alone, so the worst case is
	// a handover on the first failed renewal — a delayed deployment, not a lost one.
	if err := argo.State.ClaimTask(newTask.Id); err != nil {
		slog.Warn("Failed to claim the new task for this replica", "error", err, "id", newTask.Id)
	}

	slog.Info("A new task was triggered", "id", newTask.Id)
	for index, value := range newTask.Images {
		slog.Info("Task image expecting tag", "index", index, "tag", value.Tag, "app", task.App, "id", newTask.Id)
	}

	argo.metrics.AddAcceptedDeployment()
	return newTask, nil
}

// detectRollback returns the ID of the most recent earlier task this deployment
// rolls back to, or an empty string when it is not one. A rollback's image set was
// deployed successfully at some earlier point for the app AND differs from the
// current version — redeploying the current version is not a rollback.
func (argo *Argo) detectRollback(task models.Task) (string, error) {
	deployed, _, err := argo.State.GetTasks(models.TaskFilter{
		EndTime: float64(time.Now().Unix()),
		App:     task.App,
		Status:  models.StatusDeployedMessage,
		Limit:   rollbackHistoryWindow,
	})
	if err != nil {
		// Treated as an empty history, this would record IsRollback=false for a task
		// the backend never actually answered for.
		return "", fmt.Errorf("%w of %q: %w", ErrTaskHistoryUnavailable, task.App, err)
	}
	if len(deployed) == 0 {
		return "", nil
	}

	target := imageSignature(task)

	// GetTasks orders by created DESC, so deployed[0] is the current version.
	// Matching it means we are redeploying the current version, not rolling back.
	if imageSignature(deployed[0]) == target {
		return "", nil
	}

	for _, previous := range deployed[1:] {
		if imageSignature(previous) == target {
			return previous.Id, nil
		}
	}

	return "", nil
}

// imageSignature returns a key for a task's image set that is independent of the
// order the images arrived in.
func imageSignature(task models.Task) string {
	return strings.Join(normalizeImages(task.ListImages()), ",")
}

// GetTasks retrieves tasks from the state. Listing is deliberately NOT gated on
// ArgoCD reachability: coupling the read to a live `session/userinfo` call would
// hang the whole list on the API retry budget during an outage and then hide
// existing tasks behind an error. /readyz probes only the state backend.
func (argo *Argo) GetTasks(filter models.TaskFilter) models.TasksResponse {
	tasks, total, err := argo.State.GetTasks(filter)
	if err != nil {
		// Reported in the body rather than as an empty page, as GetAppSummaries
		// does: a reader cannot otherwise tell an outage from a quiet estate.
		slog.Error("Failed to read tasks", "error", err)
		return models.TasksResponse{Error: tasksFailedMessage}
	}

	return models.TasksResponse{
		Tasks: tasks,
		Total: total,
	}
}

// tasksFailedMessage is the client-facing text for a failed task read. The real
// cause stays in the server log, as appSummariesFailedMessage does.
const tasksFailedMessage = "failed to read tasks"

// appSummariesFailedMessage is the client-facing text for a failed aggregate.
// The real cause stays in the server log, as internalErrorMessage does for the
// handlers.
const appSummariesFailedMessage = "failed to read app summaries"

// GetAppSummaries aggregates the filter's window per application. Like
// GetTasks, it is not gated on ArgoCD reachability: it reads stored history.
func (argo *Argo) GetAppSummaries(filter models.TaskFilter) models.AppSummariesResponse {
	summaries, err := argo.State.GetAppSummaries(filter)
	if err != nil {
		// The state layer logs the cause, which names the database and its
		// schema; this body is served to any reader of the overview.
		return models.AppSummariesResponse{
			Apps:  []models.AppSummary{},
			Error: appSummariesFailedMessage,
		}
	}

	return models.AppSummariesResponse{
		Apps:      summaries,
		TotalApps: len(summaries),
	}
}

// SimpleHealthCheck checks the state backend only, never ArgoCD.
func (argo *Argo) SimpleHealthCheck() bool {
	return argo.State.Check()
}
