package models

const (
	StatusAppNotFoundMessage       = "app not found"
	StatusInProgressMessage        = "in progress"
	StatusFailedMessage            = "failed"
	StatusAborted                  = "aborted"
	StatusArgoCDUnavailableMessage = "argocd is unavailable"
	StatusConnectionUnavailable    = "cannot connect to database"
	StatusArgoCDFailedLogin        = "failed to login to argocd"
	StatusDeployedMessage          = "deployed"
	StatusAccepted                 = "accepted"
	// StatusCancelledMessage marks a deployment that was superseded by a newer
	// deployment for the same application before it reached a final state. The
	// watcher stops polling ArgoCD for the superseded task to avoid wasting API
	// calls on a rollout nobody is waiting for anymore.
	StatusCancelledMessage = "cancelled"
)

// allowedTaskStatusFilters lists every status string the /api/v1/tasks
// endpoint accepts as a `status` query parameter. Any other value is
// rejected at the HTTP boundary so callers cannot probe arbitrary strings
// (which would always return zero rows but still load the database).
//
// The map is unexported so callers cannot mutate the allowlist; use
// IsAllowedTaskStatus to check membership.
var allowedTaskStatusFilters = map[string]struct{}{
	StatusAppNotFoundMessage:       {},
	StatusInProgressMessage:        {},
	StatusFailedMessage:            {},
	StatusAborted:                  {},
	StatusArgoCDUnavailableMessage: {},
	StatusConnectionUnavailable:    {},
	StatusArgoCDFailedLogin:        {},
	StatusDeployedMessage:          {},
	StatusAccepted:                 {},
	StatusCancelledMessage:         {},
}

// IsAllowedTaskStatus reports whether the given status string is accepted
// as a value for the `/api/v1/tasks` `status` query parameter.
func IsAllowedTaskStatus(status string) bool {
	_, ok := allowedTaskStatusFilters[status]
	return ok
}

// failedTaskStatuses lists the statuses of a task whose deployment did not
// land. They are grouped for the per-app summary, so an operator scanning for
// trouble sees an aborted or unreachable-ArgoCD task alongside an outright
// failure rather than filed as neither.
var failedTaskStatuses = map[string]struct{}{
	StatusFailedMessage:            {},
	StatusAborted:                  {},
	StatusArgoCDUnavailableMessage: {},
	StatusConnectionUnavailable:    {},
	StatusArgoCDFailedLogin:        {},
}

// IsFailedTaskStatus reports whether the status means the deployment failed.
func IsFailedTaskStatus(status string) bool {
	_, ok := failedTaskStatuses[status]
	return ok
}

// FailedTaskStatuses returns the failed statuses, for building a SQL predicate.
func FailedTaskStatuses() []string {
	statuses := make([]string, 0, len(failedTaskStatuses))
	for status := range failedTaskStatuses {
		statuses = append(statuses, status)
	}
	return statuses
}
