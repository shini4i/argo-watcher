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
	Id           string  `json:"id,omitempty"`
	Created      float64 `json:"created,omitempty"`
	Updated      float64 `json:"updated,omitempty"`
	App          string  `json:"app" binding:"required" example:"argo-watcher"`
	Author       string  `json:"author" binding:"required" example:"John Doe"`
	Project      string  `json:"project" binding:"required" example:"Demo"`
	Images       []Image `json:"images" binding:"required"`
	Status       string  `json:"status,omitempty"`
	StatusReason string  `json:"status_reason,omitempty"`
	Validated    bool    `json:"validated,omitempty"`
	Timeout      int     `json:"timeout,omitempty"`
	IsRollback   bool    `json:"is_rollback,omitempty"`
	// Refresh optionally overrides the instance-wide ARGO_REFRESH_APP setting for this task.
	// A nil pointer (field omitted) keeps the instance default, so old clients are unaffected;
	// an explicit true/false forces a refresh on or off for this deployment (issue #334).
	Refresh *bool `json:"refresh,omitempty" example:"false"`
	// RollbackTargetId is the ID of the most recent earlier task whose image set
	// this deployment returns to. Empty when the deployment is not a rollback.
	RollbackTargetId string         `json:"rollback_target_id,omitempty"`
	SavedAppStatus   SavedAppStatus `json:"-"`
}

// ListImages returns the task's images formatted as "{image}:{tag}".
func (task *Task) ListImages() []string {
	list := make([]string, len(task.Images))
	for index := range task.Images {
		list[index] = fmt.Sprintf("%s:%s", task.Images[index].Image, task.Images[index].Tag)
	}
	return list
}

// IsAppNotFoundError reports whether err means ArgoCD does not have this app.
func (task *Task) IsAppNotFoundError(err error) bool {
	var appNotFoundError = fmt.Sprintf("applications.argoproj.io \"%s\" not found", task.App)

	// Since ArgoCD 2.6.7 a missing app can also surface as "permission denied"
	// (argoproj/argo-cd#13000, closed but not actually fixed), so treat both as not-found.
	return strings.Contains(err.Error(), appNotFoundError) || strings.Contains(err.Error(), "permission denied")
}

type TasksResponse struct {
	Tasks []Task `json:"tasks"`
	Error string `json:"error,omitempty"`
	Total int64  `json:"total,omitempty"`
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
