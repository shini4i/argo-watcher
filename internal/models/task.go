package models

type Image struct {
	Image string `json:"image" example:"ghcr.io/shini4i/argo-watcher"`
	Tag   string `json:"tag" example:"dev"`
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
}

type TasksResponse struct {
	Tasks []Task `json:"tasks"`
	Error string `json:"error,omitempty"`
}

type HealthStatus struct {
	Status string `json:"status"`
}

type TaskStatus struct {
	Id     string `json:"id,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type ArgoApiErrorResponse struct {
	Error   string `json:"error"`
	Code    int32  `json:"code"`
	Message string `json:"message"`
}
