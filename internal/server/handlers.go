package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state"
)

var version = "local"

// maxTaskListLimit caps the page size accepted by GET /api/v1/tasks. The
// underlying backends treat limit <= 0 as "no LIMIT clause", which would let
// any caller drain the entire task table in a single request. The cap is
// applied at the HTTP boundary so the data layer stays simple.
const maxTaskListLimit = 1000

const (
	unauthorizedMessage = "You are not authorized to perform this action"
	// oidcHeader carries the OIDC bearer token for privileged
	// (deploy-lock/rollback) requests.
	oidcHeader = "Oidc-Authorization"
)

// getVersion godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
// @Failure 401 {object} models.TaskStatus "no credential, or the credential was rejected (only when OIDC auth is enabled)"
// @Failure 503 {object} models.TaskStatus "the OIDC provider could not be consulted; retry"
// @Router /api/v1/version [get]
func (env *Env) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version)
}

// addTask godoc
// @Summary Add a new task
// @Description Add a new task
// @Tags backend
// @Accept json
// @Produce json
// @Param task body models.Task true "Task"
// @Success 202 {object} models.TaskStatus
// @Failure 401 {object} models.TaskStatus
// @Failure 406 {object} models.TaskStatus
// @Router /api/v1/tasks [post]
func (env *Env) addTask(w http.ResponseWriter, r *http.Request) {
	var task models.Task

	err := bindJSON(r, &task)
	if err != nil {
		slog.Error("failed to parse task payload", "error", err)
		writeJSON(w, http.StatusNotAcceptable, models.TaskStatus{
			Status: "invalid payload",
			Error:  err.Error(),
		})
		return
	}

	// reject deploys while a lockdown (manual or scheduled) is active
	if env.lockdown.IsLocked() {
		slog.Warn("deploy lock is set, rejecting the task")
		writeJSON(w, http.StatusNotAcceptable, models.TaskStatus{
			Status: "rejected",
			Error:  "lockdown is active, deployments are not accepted",
		})
		return
	}

	tokenValid, err := env.validateToken(r, "")
	if err != nil {
		// A non-nil error means the strategy was invoked and rejected the
		// token: a client mistake, not a server failure. Return 401 with
		// the reason so the client can show something actionable.
		slog.Warn("rejecting task", "error", err)
		writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  err.Error(),
		})
		return
	}

	task.Validated = tokenValid

	// Resolve the rollout window now, while this replica's configuration is the one
	// that applies. Left at zero it would mean "whatever the default is", and a task
	// resumed by a replica configured differently — during a rollout that changes
	// DEPLOYMENT_TIMEOUT, say — would silently be judged against the new value
	// instead of the one it was accepted under.
	if task.Timeout <= 0 {
		task.Timeout = int(env.config.DeploymentTimeout)
	}

	newTask, err := env.argo.AddTask(task)
	if err != nil {
		slog.Error("failed to add task", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, models.TaskStatus{
			Status: "down",
			Error:  err.Error(),
		})
		return
	}

	go env.updater.WaitForRollout(*newTask, false)

	writeJSON(w, http.StatusAccepted, models.TaskStatus{
		Id:     newTask.Id,
		Status: models.StatusAccepted,
	})
}

// getState godoc
// @Summary Get state content
// @Description Get all tasks that match the provided parameters
// @Tags backend, frontend
// @Param app query string false "App name"
// @Param status query string false "Task status (e.g. 'in progress', 'failed', 'deployed', 'cancelled')"
// @Param from_timestamp query int true "From timestamp" default(1648390029)
// @Param to_timestamp query int false "To timestamp"
// @Param limit query int false "Maximum number of tasks to return (1-1000, defaults to 1000)"
// @Param offset query int false "Number of tasks to skip before returning results"
// @Success 200 {object} models.TasksResponse
// @Failure 401 {object} models.TaskStatus "no credential, or the credential was rejected (only when OIDC auth is enabled)"
// @Failure 503 {object} models.TaskStatus "the OIDC provider could not be consulted; retry"
// @Router /api/v1/tasks [get]
func (env *Env) getState(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	startTime, err := strconv.ParseFloat(query.Get("from_timestamp"), 64)
	if err != nil && query.Get("from_timestamp") != "" {
		slog.Debug("invalid from_timestamp, defaulting to 0", "from_timestamp", query.Get("from_timestamp"))
	}
	endTime, err := strconv.ParseFloat(query.Get("to_timestamp"), 64)
	if err != nil && query.Get("to_timestamp") != "" {
		slog.Debug("invalid to_timestamp, defaulting to current time", "to_timestamp", query.Get("to_timestamp"))
	}
	if endTime == 0 {
		endTime = float64(time.Now().Unix())
	}
	app := query.Get("app")
	status := query.Get("status")
	if status != "" && !models.IsAllowedTaskStatus(status) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported status filter"})
		return
	}

	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil && query.Get("limit") != "" {
		slog.Debug("invalid limit, defaulting to 0", "limit", query.Get("limit"))
	}
	offset, err := strconv.Atoi(query.Get("offset"))
	if err != nil && query.Get("offset") != "" {
		slog.Debug("invalid offset, defaulting to 0", "offset", query.Get("offset"))
	}
	if limit <= 0 || limit > maxTaskListLimit {
		limit = maxTaskListLimit
	}
	if offset < 0 {
		offset = 0
	}

	writeJSON(w, http.StatusOK, env.argo.GetTasks(startTime, endTime, app, status, limit, offset))
}

// getTaskStatus godoc
// @Summary Get the status of a task
// @Description Get the status of a task
// @Param id path string true "Task id" default(9185fae0-add5-11ec-87f3-56b185c552fa)
// @Tags backend
// @Produce json
// @Success 200 {object} models.TaskStatus
// @Failure 404 {object} models.TaskStatus
// @Failure 500 {object} models.TaskStatus
// @Router /api/v1/tasks/{id} [get]
func (env *Env) getTaskStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := env.argo.State.GetTask(id)

	if err != nil {
		if errors.Is(err, state.ErrTaskNotFound) {
			writeJSON(w, http.StatusNotFound, models.TaskStatus{
				Id:    id,
				Error: "task not found",
			})
			return
		}
		// Any other error is a backend failure (e.g. the database is
		// unreachable). Return 500 so it surfaces in metrics and alerting
		// instead of masquerading as a missing task, and keep the internal
		// detail out of the client response.
		slog.Error("failed to retrieve task", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, models.TaskStatus{
			Id:    id,
			Error: "internal server error",
		})
	} else {
		setTaskApp(r, task.MetricApp())
		writeJSON(w, http.StatusOK, models.TaskStatus{
			Id:           task.Id,
			Created:      task.Created,
			Updated:      task.Updated,
			App:          task.App,
			Author:       task.Author,
			Project:      task.Project,
			Images:       task.Images,
			Status:       task.Status,
			StatusReason: task.StatusReason,
		})
	}
}

// Causes reported by the readiness probe. They are the response body only; the
// status code is what an orchestrator acts on.
const (
	reasonDraining         = "shutting down"
	reasonStateUnreachable = "state backend unreachable"
)

// livez godoc
// @Summary Liveness probe
// @Description Report whether the process itself is still serving. It checks no dependency, because a restart — the only remedy a failing liveness probe triggers — cannot fix one.
// @Tags service
// @Produce json
// @Success 200 {object} models.HealthStatus
// @Router /livez [get]
func (env *Env) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, models.HealthStatus{Status: "up"})
}

// readyz godoc
// @Summary Readiness probe
// @Description Report whether this instance should receive traffic: down while shutting down, and down when the state backend is unreachable. ArgoCD reachability is deliberately excluded — the API and Web UI must keep serving task history and the unreachable banner during an ArgoCD outage (see /api/v1/reachability).
// @Tags service
// @Produce json
// @Success 200 {object} models.HealthStatus
// @Failure 503 {object} models.HealthStatus
// @Router /readyz [get]
func (env *Env) readyz(w http.ResponseWriter, _ *http.Request) {
	// Ordered so a drain is reported as a drain: once shutdown starts the state
	// backend is irrelevant, and reporting it would mislabel a planned rollout as an
	// outage.
	if env.isDraining() {
		writeJSON(w, http.StatusServiceUnavailable, models.HealthStatus{Status: "down", Reason: reasonDraining})
		return
	}

	if !env.argo.SimpleHealthCheck() {
		writeJSON(w, http.StatusServiceUnavailable, models.HealthStatus{Status: "down", Reason: reasonStateUnreachable})
		return
	}

	writeJSON(w, http.StatusOK, models.HealthStatus{Status: "up"})
}

// getConfig godoc
// @Summary Get the configuration of the server (excluding sensitive data)
// @Description Get the configuration of the server (excluding sensitive data)
// @Tags backend
// @Produce json
// @Success 200 {object} config.ServerConfig
// @Router /api/v1/config [get]
func (env *Env) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, env.config)
}

// requireOIDCAuth validates the OIDC token from the Oidc-Authorization header when
// OIDC auth is enabled. It returns true if validation passes (or OIDC is disabled).
// On failure the response distinguishes:
//   - 401 with "authentication required" when no auth header was sent.
//   - 401 with the strategy's reason when the token was rejected.
//   - 503 when the provider could not be consulted at all, so that a provider
//     outage does not make the Web UI treat the session as dead (see
//     requireAuthenticatedRead). Details land in the server log only.
func (env *Env) requireOIDCAuth(w http.ResponseWriter, r *http.Request) bool {
	if !env.config.OIDC.Enabled {
		return true
	}

	valid, err := env.validateToken(r, oidcHeader)
	if valid {
		return true
	}
	if errors.Is(err, auth.ErrProviderUnavailable) {
		slog.Error("rejecting request: authentication provider unavailable",
			"method", r.Method, "url", r.URL, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, models.TaskStatus{
			Status: "authentication provider unavailable",
			Error:  err.Error(),
		})
		return false
	}
	if err != nil {
		slog.Warn("rejected request with invalid token",
			"method", r.Method, "url", r.URL, "error", err)
		writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  err.Error(),
		})
		return false
	}

	slog.Warn("rejected unauthenticated request", "method", r.Method, "url", r.URL)
	writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
		Status: unauthorizedMessage,
		Error:  "authentication required (set " + oidcHeader + " header)",
	})
	return false
}

// requireAuthenticatedRead returns middleware that rejects reads carrying no valid
// credential once OIDC auth is enabled; with OIDC disabled it is a no-op.
//
// Any configured credential is accepted — an OIDC session, the deploy token or a CI
// JWT — and reads are deliberately not restricted to OIDC_PRIVILEGED_GROUPS, which
// gates the deploy-lock writes alone.
//
// A rejected or missing credential is 401; a provider that could not be consulted is
// 503, because the Web UI discards its session on a 401.
func (env *Env) requireAuthenticatedRead() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !env.config.OIDC.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			valid, err := env.authenticator.AuthenticateRequest(r)
			if valid {
				next.ServeHTTP(w, r)
				return
			}

			if errors.Is(err, auth.ErrProviderUnavailable) {
				slog.Error("rejecting read: authentication provider unavailable",
					"method", r.Method, "url", r.URL, "error", err)
				writeJSON(w, http.StatusServiceUnavailable, models.TaskStatus{
					Status: "authentication provider unavailable",
					Error:  err.Error(),
				})
				return
			}

			if err != nil {
				slog.Warn("rejecting read with invalid credential",
					"method", r.Method, "url", r.URL, "error", err)
				writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
					Status: unauthorizedMessage,
					Error:  err.Error(),
				})
				return
			}

			slog.Warn("rejecting unauthenticated read", "method", r.Method, "url", r.URL)
			writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
				Status: unauthorizedMessage,
				Error:  "authentication required (set " + oidcHeader + " header)",
			})
		})
	}
}

// countUnauthenticatedRead returns middleware that counts reads arriving with no
// credential while OIDC auth is enabled. It never blocks: the route it guards is open
// on purpose, and the count is the migration signal for closing it later.
//
// It tests for presence rather than validity so the guarded endpoint — polled for the
// whole length of every deployment — never waits on the provider, and so an expired
// token cannot inflate the number whose fall to zero licenses that closure.
//
// The count is recorded after the handler, which is what names the application behind
// the read: a fleet-wide total says a migration is unfinished, the app label says
// whose pipeline to go and upgrade.
func (env *Env) countUnauthenticatedRead() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !env.config.OIDC.Enabled || env.hasCredential(r) {
				next.ServeHTTP(w, r)
				return
			}

			labelled, holder := withTaskAppLabel(r)

			next.ServeHTTP(w, labelled)

			app := holder.app
			if app == "" {
				app = models.UnknownApp
			}
			env.metrics.AddUnauthenticatedRead(routePattern(labelled), app)
		})
	}
}

// hasCredential reports whether the request carries a non-empty value in any header a
// configured auth strategy reads, saying nothing about whether it is valid.
func (env *Env) hasCredential(request *http.Request) bool {
	for header := range env.strategies {
		if request.Header.Get(header) != "" {
			return true
		}
	}

	return false
}

// validateToken validates the incoming request using the configured authentication strategies.
// When allowedAuthStrategy is empty, the validation delegates to the default authenticator,
// which returns the last validation error when no strategies succeed. When allowedAuthStrategy
// is provided, validation is restricted to that specific strategy header via the authenticator.
func (env *Env) validateToken(r *http.Request, allowedAuthStrategy string) (bool, error) {
	if allowedAuthStrategy == "" {
		return env.authenticator.Validate(r)
	}

	return env.authenticator.ValidateStrategy(r, allowedAuthStrategy)
}
