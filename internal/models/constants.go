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
)

// AllowedTaskStatusFilters lists every status string the /api/v1/tasks
// endpoint accepts as a `status` query parameter. Any other value is
// rejected at the HTTP boundary so callers cannot probe arbitrary strings
// (which would always return zero rows but still load the database).
var AllowedTaskStatusFilters = map[string]struct{}{
	StatusAppNotFoundMessage:       {},
	StatusInProgressMessage:        {},
	StatusFailedMessage:            {},
	StatusAborted:                  {},
	StatusArgoCDUnavailableMessage: {},
	StatusConnectionUnavailable:    {},
	StatusArgoCDFailedLogin:        {},
	StatusDeployedMessage:          {},
	StatusAccepted:                 {},
}
