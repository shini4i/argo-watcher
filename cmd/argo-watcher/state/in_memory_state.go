package state

import (
	"errors"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

type InMemoryState struct {
	tasks []models.Task
}

// Connect is a placeholder method that does not establish any connection.
// It logs a debug message indicating that the InMemoryState does not connect to anything and skips the connection process.
// This method exists to fulfill the State interface requirement and has no functional value.
func (state *InMemoryState) Connect(serverConfig *config.ServerConfig) error {
	log.Debug().Msg("InMemoryState does not connect to anything. Skipping.")
	return nil
}

// Add adds a new task to the in-memory state.
// It takes a models.Task parameter and updates the task's created timestamp and status.
// The method appends the task to the list of tasks in the in-memory state.
// It always returns nil as there is no error handling in the in-memory implementation.
func (state *InMemoryState) Add(task models.Task) (*models.Task, error) {
	task.Id = uuid.New().String()
	task.Created = float64(time.Now().Unix())
	task.Updated = float64(time.Now().Unix())
	task.Status = models.StatusInProgressMessage
	state.tasks = append(state.tasks, task)
	return &task, nil
}

// GetTasks retrieves tasks from the in-memory state based on the provided time range and app filter.
// It takes a start time, end time, and optional app filter.
// If there are no tasks in the in-memory state, it returns an empty slice.
// The method filters tasks within the time range and, optionally, by app.
// If an app filter is provided, only matching tasks are included.
func (state *InMemoryState) GetTasks(startTime float64, endTime float64, app string) []models.Task {
	if state.tasks == nil {
		return []models.Task{}
	}

	var tasks []models.Task

	for _, task := range state.tasks {
		if task.Created >= startTime && task.Created <= endTime {
			if app == "" {
				tasks = append(tasks, task)
			} else if app == task.App {
				tasks = append(tasks, task)
			}
		}
	}

	if tasks == nil {
		return []models.Task{}
	}

	return tasks
}

// GetTask retrieves a task from the in-memory state based on the provided task ID.
// It takes a string parameter for the task ID.
// The method iterates over the tasks in the in-memory state and returns the task if a matching ID is found.
// If no task with the given ID is found, it returns an error indicating that the task was not found.
func (state *InMemoryState) GetTask(id string) (*models.Task, error) {
	for _, task := range state.tasks {
		if task.Id == id {
			return &task, nil
		}
	}
	return nil, errors.New("task not found")
}

// SetTaskStatus updates the status and status reason of a task in the in-memory state based on the provided task ID.
// It takes a string parameter for the task ID, status, and reason.
// The method iterates over the tasks in the in-memory state and updates the task with the matching ID.
// Note that this method does not perform any error handling if the task ID is not found.
func (state *InMemoryState) SetTaskStatus(id string, status string, reason string) error {
	for idx, task := range state.tasks {
		if task.Id == id {
			state.tasks[idx].Status = status
			state.tasks[idx].StatusReason = reason
			state.tasks[idx].Updated = float64(time.Now().Unix())
		}
	}
	return nil
}

// GetAppList retrieves a list of unique app names from the tasks in the in-memory state.
// It returns a slice of strings containing the app names.
// The method iterates over the tasks in the in-memory state and adds unique app names to the list.
// The list of app names is returned as a slice.
// If there are no tasks in the in-memory state, an empty slice is returned.
func (state *InMemoryState) GetAppList() []string {
	var apps []string

	for _, app := range state.tasks {
		if !helpers.Contains(apps, app.App) {
			apps = append(apps, app.App)
		}
	}

	if apps == nil {
		return []string{}
	}

	return apps
}

// Check is a placeholder method that implements the Check() bool interface.
// It always returns true and does not perform any actual state checking.
// This method exists to fulfill the State interface requirement and has no functional value.
func (state *InMemoryState) Check() bool {
	return true
}

// ProcessObsoleteTasks scans the in-memory tasks for obsolete tasks and updates their status.
// It starts a process to watch for obsolete tasks by invoking the `processInMemoryObsoleteTasks` function.
// The function uses the `retry` package to periodically retry the task processing with a fixed delay of 60 minutes.
func (state *InMemoryState) ProcessObsoleteTasks(retryTimes uint) {
	log.Debug().Msg("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			state.tasks = processInMemoryObsoleteTasks(state.tasks)
			return errDesiredRetry
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(60*time.Minute),
		retry.Attempts(retryTimes),
	)
	if err != nil {
		log.Error().Msgf("Couldn't process obsolete tasks. Got the following error: %s", err)
	}
}

// processInMemoryObsoleteTasks processes a list of tasks and updates their status based on specific conditions.
// It takes a slice of models.Task as input and returns a new slice of updated tasks.
// The function iterates over the tasks and checks for specific status conditions.
// If a task has a status of "app not found", it is skipped and excluded from the updated tasks.
// If a task has a status of "in progress" and the updated timestamp plus 3600 seconds is less than the current Unix timestamp,
// the task's status is updated to "aborted".
// The function collects the updated tasks and returns them as a new slice.
func processInMemoryObsoleteTasks(tasks []models.Task) []models.Task {
	var updatedTasks []models.Task
	for _, task := range tasks {
		if task.Status == models.StatusAppNotFoundMessage {
			continue
		}
		if task.Status == models.StatusInProgressMessage && task.Updated+3600 < float64(time.Now().Unix()) {
			task.Status = models.StatusAborted
		}
		updatedTasks = append(updatedTasks, task)
	}
	return updatedTasks
}
