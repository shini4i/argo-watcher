package models

type Application struct {
	Status struct {
		Sync struct {
			Status string `json:"status"`
		}
		Health struct {
			Status string `json:"status"`
		}
		Summary struct {
			Images []string `json:"images"`
		}
	} `json:"status"`
}
