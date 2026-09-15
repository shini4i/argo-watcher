package models

// RecentOutcomeLimit caps how many recent task statuses AppSummary reports per
// application, matching the outcome strip the Web UI draws.
const RecentOutcomeLimit = 10

// AppSummary aggregates one application's tasks inside a time window. Counts are
// exact for the window — unlike client-side grouping over a single page, which
// can only report lower bounds.
type AppSummary struct {
	App string `json:"app"`
	// Project is the newest task's project name, not a per-app constant.
	Project string `json:"project"`
	// Total is every task for the app in the window; the three counters below
	// classify them and need not sum to Total (a cancelled task is in none).
	Total    int64 `json:"total"`
	Failed   int64 `json:"failed"`
	Running  int64 `json:"running"`
	Deployed int64 `json:"deployed"`
	// MedianDurationSeconds is the median of updated-created over the window's
	// settled tasks, or 0 when none have settled.
	MedianDurationSeconds float64 `json:"median_duration_seconds"`
	// LastCreated, LastStatus and LastStatusReason describe the newest task.
	LastCreated      float64 `json:"last_created"`
	LastStatus       string  `json:"last_status"`
	LastStatusReason string  `json:"last_status_reason,omitempty"`
	// RecentStatuses lists up to RecentOutcomeLimit statuses, newest first.
	RecentStatuses []string `json:"recent_statuses"`
}

// AppSummariesResponse is the payload of GET /api/v1/apps/summary.
type AppSummariesResponse struct {
	Apps []AppSummary `json:"apps"`
	// TotalApps is how many applications the window covers, which equals
	// len(Apps): the summary is not paginated.
	TotalApps int    `json:"total_apps"`
	Error     string `json:"error,omitempty"`
}
