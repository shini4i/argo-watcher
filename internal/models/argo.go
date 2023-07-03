package models

import "fmt"

type Application struct {
	Status struct {
		Health struct {
			Status string `json:"status"`
		}
		OperationState struct {
			Phase      string `json:"phase"`
			Message    string `json:"message"`
			SyncResult struct {
				Resources []struct {
					HookPhase string `json:"hookPhase"` // example: Failed
					HookType  string `json:"hookType"`  // example: PreSync
					Kind      string `json:"kind"`      // example: Pod | Job
					Message   string `json:"message"`   // example: Job has reached the specified backoff limit
					Status    string `json:"status"`    // example: Synced
					SyncPhase string `json:"syncPhase"` // example: PreSync
					Name      string `json:"name"`      // example: app-migrations
					Namespace string `json:"namespace"` // example: app
				} `json:"resources"`
			} `json:"syncResult"`
		} `json:"operationState"`
		Resources []struct {
			Kind      string `json:"kind"`      // example: Pod | Job
			Name      string `json:"name"`      // example: app-migrations
			Namespace string `json:"namespace"` // example: app
			Health    struct {
				Message string `json:"message"` // example: Job has reached the specified backoff limit
				Status  string `json:"status"`  // example: Synced
			} `json:"health"`
		} `json:"resources"`
		Summary struct {
			Images []string `json:"images"`
		}
		Sync struct {
			Status string `json:"status"`
		}
	} `json:"status"`
}

// ListSyncResultResources returns a list of strings representing the sync result resources of the application.
// Each string in the list contains information about the resource's kind, name, hook type, hook phase, and message.
// The information is formatted as "{kind}({name}) {hookType} {hookPhase} with message {message}".
// The list is generated based on the Application's status and its operation state's sync result resources.
func (app *Application) ListSyncResultResources() []string {
	list := make([]string, len(app.Status.OperationState.SyncResult.Resources))
	for index := range app.Status.OperationState.SyncResult.Resources {
		resource := app.Status.OperationState.SyncResult.Resources[index]
		list[index] = fmt.Sprintf("%s(%s) %s %s with message %s", resource.Kind, resource.Name, resource.HookType, resource.HookPhase, resource.Message)
	}
	return list
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
		message := fmt.Sprintf("%s(%s) %s", resource.Kind, resource.Name, resource.Health.Status)
		if resource.Health.Message != "" {
			message += " with message " + resource.Health.Message
		}
		list = append(list, message)
	}
	return list
}

type Userinfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username"`
}
