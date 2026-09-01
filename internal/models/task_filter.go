package models

// TaskFilter narrows a task list query. Every field is optional and a zero
// value means "no constraint", except EndTime: it is the inclusive upper bound
// of the creation window, so a zero value selects nothing.
type TaskFilter struct {
	// StartTime is the exclusive lower bound of the task creation window, in
	// Unix seconds.
	StartTime float64
	// EndTime is the inclusive upper bound of the task creation window, in Unix
	// seconds.
	EndTime float64
	// App matches the application name exactly.
	App string
	// Status matches the task status exactly.
	Status string
	// Search matches a case-insensitive substring of the app name, the author,
	// or any image formatted as "image:tag". See Task.MatchesSearch.
	Search string
	// Limit caps how many tasks are returned; 0 means no cap.
	Limit int
	// Offset is how many matching tasks to skip before returning results.
	Offset int
}
