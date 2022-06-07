package models

type Image struct {
	Image string `json:"image"`
	Tag   string `json:"tag"`
}

type Task struct {
	Id      string  `json:"id,omitempty"`
	Created float64 `json:"created,omitempty"`
	Updated float64 `json:"updated,omitempty"`
	App     string  `json:"app"`
	Author  string  `json:"author"`
	Project string  `json:"project"`
	Images  []Image `json:"images"`
	Status  string  `json:"status,omitempty"`
}

type HealthStatus struct {
	Status string `json:"status"`
}

type TaskStatus struct {
	Id     string `json:"id,omitempty"`
	Status string `json:"status"`
}
