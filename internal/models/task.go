package models

import (
	"fmt"
	"strings"
)

type Image struct {
	Image string `json:"image" example:"ghcr.io/shini4i/argo-watcher"`
	Tag   string `json:"tag" example:"dev"`
}

type Task struct {
	Id             string  `json:"id,omitempty"`
	Created        float64 `json:"created,omitempty"`
	Updated        float64 `json:"updated,omitempty"`
	App            string  `json:"app" binding:"required" example:"argo-watcher"`
	Author         string  `json:"author" binding:"required" example:"John Doe"`
	Project        string  `json:"project" binding:"required" example:"Demo"`
	Images         []Image `json:"images" binding:"required"`
	Status         string  `json:"status,omitempty"`
	StatusReason   string  `json:"status_reason,omitempty"`
	Validated      bool    `json:"validated,omitempty"`
	SavedAppStatus string  `json:"-"`
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
	return strings.Contains(err.Error(), appNotFoundError)
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
