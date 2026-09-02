package models

import (
	"fmt"
	"strings"
)

type Image struct {
	Image string `json:"image" binding:"max=255" example:"ghcr.io/shini4i/argo-watcher"`
	Tag   string `json:"tag" binding:"max=255" example:"dev"`
}

type SavedAppStatus struct {
	Status     string `json:"app_status"`
	ImagesHash []byte `json:"app_hash"`
}

// MaxTaskImages is the number of images one task may carry, enforced by the `max`
// binding tag on Task.Images. Submission takes no credential and every image is
// matched against the application on every poll, so the list is bounded (issue #562).
const MaxTaskImages = 50

// MaxTaskFieldLength bounds every free-text field of a submission, enforced by the
// `max` binding tags below. An oversized name is stored once and re-served to every
// reader of the task list; the fields it caps are Kubernetes and Argo CD names.
const MaxTaskFieldLength = 255

type Task struct {
	// Id, Created and Updated are server-owned: the state backend stamps all three,
	// and AddTask additionally clears Updated, which the backend would otherwise leave
	// carrying a submitted value into the start notification.
	Id      string  `json:"id,omitempty"`
	Created float64 `json:"created,omitempty"`
	Updated float64 `json:"updated,omitempty"`
	App     string  `json:"app" binding:"required,max=255" example:"argo-watcher"`
	Author  string  `json:"author" binding:"required,max=255" example:"John Doe"`
	Project string  `json:"project" binding:"required,max=255" example:"Demo"`
	Images  []Image `json:"images" binding:"required,max=50,dive"`
	Status  string  `json:"status,omitempty"`
	// StatusReason is server-owned: it is written when a deployment reaches a terminal
	// state, so AddTask discards a submitted value before the task is stored or notified.
	StatusReason string `json:"status_reason,omitempty"`
	// Validated records whether the request that created this task presented a valid
	// credential. It gates the git write-back and, since it also decides what a task
	// may supersede, it is never accepted from or served over the API.
	Validated  bool `json:"-"`
	Timeout    int  `json:"timeout,omitempty"`
	IsRollback bool `json:"is_rollback,omitempty"`
	// Refresh optionally overrides the instance-wide ARGO_REFRESH_APP setting for this task.
	// A nil pointer (field omitted) keeps the instance default, so old clients are unaffected;
	// an explicit true/false forces a refresh on or off for this deployment (issue #334).
	Refresh *bool `json:"refresh,omitempty" example:"false"`
	// RollbackTargetId is the ID of the most recent earlier task whose image set
	// this deployment returns to. Empty when the deployment is not a rollback.
	RollbackTargetId string         `json:"rollback_target_id,omitempty"`
	SavedAppStatus   SavedAppStatus `json:"-"`
}

// UnknownApp is the app label used for a task whose name must not reach a metric.
const UnknownApp = "unknown"

// MetricApp returns the app name safe to use as a Prometheus label. Task submission
// accepts an arbitrary app name without a credential, so an unvalidated task reports
// UnknownApp: a caller could otherwise mint a permanent series per request. Call sites
// that only run once ArgoCD has confirmed the application exists may use App directly.
func (task *Task) MetricApp() string {
	if !task.Validated {
		return UnknownApp
	}
	return task.App
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

// HealthStatus is the payload of the probe endpoints. Status is "up" or "down";
// Reason names the cause of a "down" and is omitted otherwise, so an operator
// reading the endpoint during an incident can tell a graceful drain apart from an
// unreachable state backend.
type HealthStatus struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
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

// MatchesSearch reports whether query occurs, case-insensitively, in the task's
// app name, author, or any of its images formatted as "image:tag". An empty
// query matches every task. This is the reference for the free-text task search
// exposed by the API; the Postgres query mirrors it in SQL.
func (task *Task) MatchesSearch(query string) bool {
	if query == "" {
		return true
	}

	needle := strings.ToLower(query)
	if strings.Contains(strings.ToLower(task.App), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(task.Author), needle) {
		return true
	}
	for _, image := range task.ListImages() {
		if strings.Contains(strings.ToLower(image), needle) {
			return true
		}
	}
	return false
}
