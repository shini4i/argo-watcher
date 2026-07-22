package state

import (
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	// TaskStaleThresholdSeconds is the time in seconds after which an in-progress task is considered stale and aborted.
	TaskStaleThresholdSeconds = 3600
	// ObsoleteTaskCheckInterval is the interval between checks for obsolete tasks.
	ObsoleteTaskCheckInterval = 60 * time.Minute
	// StaleTaskAbortReason is the status reason set when an in-progress task is
	// aborted for exceeding the staleness window (distinct from an ArgoCD outage).
	StaleTaskAbortReason = "Deployment did not complete within the staleness window; marked aborted by argo-watcher."
)

// InMemoryState provides a thread-safe in-memory implementation of task storage.
// It uses a read-write mutex to protect concurrent access to the tasks slice.
type InMemoryState struct {
	mu    sync.RWMutex
	tasks []models.Task
}

var _ TaskRepository = (*InMemoryState)(nil)

// Connect is a no-op that exists only to satisfy the TaskRepository interface.
func (state *InMemoryState) Connect(serverConfig *config.ServerConfig) error {
	slog.Debug("InMemoryState does not connect to anything. Skipping.")
	return nil
}

// AddTask assigns an id, timestamps, and in-progress status, then appends the
// task. The error is always nil; in-memory storage has no persistence failure.
func (state *InMemoryState) AddTask(task models.Task) (*models.Task, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	task.Id = uuid.New().String()
	task.Created = float64(time.Now().Unix())
	task.Updated = float64(time.Now().Unix())
	task.Status = models.StatusInProgressMessage
	state.tasks = append(state.tasks, task)
	return &task, nil
}

// taskMatchesFilters reports whether a task falls within the time window
// and matches the optional app/status filters (empty values are wildcards).
// The lower bound is exclusive and the upper bound is inclusive to match the
// Postgres query (`created > startTime AND created <= endTime`).
func taskMatchesFilters(task models.Task, startTime, endTime float64, app, status string) bool {
	if task.Created <= startTime || task.Created > endTime {
		return false
	}
	if app != "" && app != task.App {
		return false
	}
	if status != "" && status != task.Status {
		return false
	}
	return true
}

// paginate returns the [offset:offset+limit] slice of tasks, clamping to bounds.
// A non-positive limit means "no upper bound" — return everything from offset onward.
func paginate(tasks []models.Task, limit, offset int) []models.Task {
	if offset >= len(tasks) {
		return []models.Task{}
	}
	end := len(tasks)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return tasks[offset:end]
}

// GetTasks retrieves tasks from the in-memory state based on the provided time range, app, and status filters.
// Empty filter values (app == "" or status == "") are treated as wildcards.
func (state *InMemoryState) GetTasks(startTime float64, endTime float64, app string, status string, limit int, offset int) ([]models.Task, int64) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	if state.tasks == nil {
		return []models.Task{}, 0
	}

	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}

	var tasks []models.Task
	for _, task := range state.tasks {
		if taskMatchesFilters(task, startTime, endTime, app, status) {
			tasks = append(tasks, task)
		}
	}

	if len(tasks) == 0 {
		return []models.Task{}, 0
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Created > tasks[j].Created
	})

	return paginate(tasks, limit, offset), int64(len(tasks))
}

// GetTask returns the task with the given id, or ErrTaskNotFound if none matches.
func (state *InMemoryState) GetTask(id string) (*models.Task, error) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	for _, task := range state.tasks {
		if task.Id == id {
			return &task, nil
		}
	}
	return nil, ErrTaskNotFound
}

// SetTaskStatus updates the status and status reason of the task with the given
// id, or returns an error if no task matches.
func (state *InMemoryState) SetTaskStatus(id, status, reason string) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	for idx, task := range state.tasks {
		if task.Id == id {
			state.tasks[idx].Status = status
			state.tasks[idx].StatusReason = reason
			state.tasks[idx].Updated = float64(time.Now().Unix())
			return nil
		}
	}
	return errors.New("task not found")
}

// CancelInProgressTasks marks in-progress tasks for the given app as cancelled
// and returns how many were updated. A task is only cancelled when it shares at
// least one image name with the supplied images (tags ignored), so independent
// per-image deployments of the same app do not cancel each other.
func (state *InMemoryState) CancelInProgressTasks(app string, images []models.Image, reason string) (int64, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	var count int64
	now := float64(time.Now().Unix())
	for idx := range state.tasks {
		if state.tasks[idx].App == app &&
			state.tasks[idx].Status == models.StatusInProgressMessage &&
			imageNamesOverlap(state.tasks[idx].Images, images) {
			state.tasks[idx].Status = models.StatusCancelledMessage
			state.tasks[idx].StatusReason = reason
			state.tasks[idx].Updated = now
			count++
		}
	}
	return count, nil
}

// Check always returns true; in-memory storage is always available.
func (state *InMemoryState) Check() bool {
	return true
}

// ProcessObsoleteTasks runs processInMemoryObsoleteTasks every
// ObsoleteTaskCheckInterval. retryTimes bounds the number of runs; 0 means run
// forever (the production case). It is meant to run in its own goroutine.
func (state *InMemoryState) ProcessObsoleteTasks(retryTimes uint) {
	slog.Debug("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			state.mu.Lock()
			defer state.mu.Unlock()
			state.tasks = processInMemoryObsoleteTasks(state.tasks)
			return errDesiredRetry
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(ObsoleteTaskCheckInterval),
		retry.Attempts(retryTimes),
	)
	if err != nil {
		slog.Error("Couldn't process obsolete tasks", "error", err)
	}
}

// processInMemoryObsoleteTasks returns tasks with "app not found" entries dropped
// and "in progress" entries older than TaskStaleThresholdSeconds marked "aborted".
func processInMemoryObsoleteTasks(tasks []models.Task) []models.Task {
	var updatedTasks []models.Task
	for _, task := range tasks {
		if task.Status == models.StatusAppNotFoundMessage {
			continue
		}
		if task.Status == models.StatusInProgressMessage && task.Updated+TaskStaleThresholdSeconds < float64(time.Now().Unix()) {
			task.Status = models.StatusAborted
			task.StatusReason = StaleTaskAbortReason
		}
		updatedTasks = append(updatedTasks, task)
	}
	return updatedTasks
}
