package models

import (
	"fmt"
	"strings"
)

type Image struct {
	Image string `json:"image" example:"ghcr.io/shini4i/argo-watcher"`
	Tag   string `json:"tag" example:"dev"`
}

type SavedAppStatus struct {
	Status     string `json:"app_status"`
	ImagesHash []byte `json:"app_hash"`
}

type Task struct {
	Id             string         `json:"id,omitempty"`
	Created        float64        `json:"created,omitempty"`
	Updated        float64        `json:"updated,omitempty"`
	App            string         `json:"app" binding:"required" example:"argo-watcher"`
	Author         string         `json:"author" binding:"required" example:"John Doe"`
	Project        string         `json:"project" binding:"required" example:"Demo"`
	Images         []Image        `json:"images" binding:"required"`
	Status         string         `json:"status,omitempty"`
	StatusReason   string         `json:"status_reason,omitempty"`
	Validated      bool           `json:"validated,omitempty"`
	SavedAppStatus SavedAppStatus `json:"-"`
}

// ListImages returns a list of strings representing the images of the task.
// Each string in the list is in the format "{image}:{tag}".
// The list is generated based on the Task's Images field.
func (task *Task) ListImages() []string {
	list := make([]string, len(task.Images))
	for index := range task.Images {
		list[index] = fmt.Sprintf("%s:%s", task.Images[index].Image, task.Images[index].Tag)
	}
	return list
}

// IsAppNotFoundError check if app not found error.
func (task *Task) IsAppNotFoundError(err error) bool {
	var appNotFoundError = fmt.Sprintf("applications.argoproj.io \"%s\" not found", task.App)

	// starting from ArgoCD 2.6.7 we are affected by this issue: https://github.com/argoproj/argo-cd/issues/13000
	// although it is closed as completed, it is not fixed, so it seems to be a mistake
	// from now on we consider "permission denied" as app not found error as this is what argocd returns in such cases
	return strings.Contains(err.Error(), appNotFoundError) || strings.Contains(err.Error(), "permission denied")
}

type TasksResponse struct {
	Tasks []Task `json:"tasks"`
	Error string `json:"error,omitempty"`
}

type HealthStatus struct {
	Status string `json:"status"`
}

type TaskStatus struct {
	Id           string  `json:"id,omitempty"`
	Created      float64 `json:"created,omitempty"`
	Updated      float64 `json:"updated,omitempty"`
	App          string  `json:"app,omitempty" binding:"required" example:"argo-watcher"`
	Author       string  `json:"author,omitempty" binding:"required" example:"John Doe"`
	Project      string  `json:"project,omitempty" binding:"required" example:"Demo"`
	Images       []Image `json:"images,omitempty" binding:"required"`
	Status       string  `json:"status,omitempty"`
	StatusReason string  `json:"status_reason,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type ArgoApiErrorResponse struct {
	Error   string `json:"error"`
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

type LockdownSchedule struct {
	Cron     string `json:"cron" example:"0 2 * * *"`
	Duration string `json:"duration" example:"2h"`
}
