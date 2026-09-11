package state

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

var errDesiredRetry = errors.New("desired retry error")

// ErrTaskNotFound is returned by TaskRepository.GetTask and SetTaskStatus when no
// task exists for the requested id. Callers use errors.Is to distinguish a genuine "not found"
// (HTTP 404) from a backend failure (HTTP 500), so a database outage is not
// silently reported as a missing task.
var ErrTaskNotFound = errors.New("task not found")

// maySupersede reports whether a deployment may cancel an in-flight task, by
// comparing the credential each one presented. Only the uncredentialed-cancels-
// credentialed direction is refused.
func maySupersede(newTaskValidated, inFlightValidated bool) bool {
	return newTaskValidated || !inFlightValidated
}

// imageNamesOverlap reports whether the two image slices share at least one
// image name (the repository, ignoring the tag). It is used to decide whether a
// new deployment supersedes an in-progress one for the same app.
func imageNamesOverlap(a, b []models.Image) bool {
	names := make(map[string]struct{}, len(a))
	for _, img := range a {
		names[img.Image] = struct{}{}
	}
	for _, img := range b {
		if _, ok := names[img.Image]; ok {
			return true
		}
	}
	return false
}

// TaskRepository defines the contract for task persistence.
type TaskRepository interface {
	Connect(serverConfig *config.ServerConfig) error
	// AddTask stores the task and returns it with the server-owned fields filled
	// in: id, in-progress status, and Created/Updated as Unix seconds.
	AddTask(task models.Task) (*models.Task, error)
	GetTasks(filter models.TaskFilter) ([]models.Task, int64)
	// GetAppSummaries aggregates the filter's time window per application. Only
	// StartTime and EndTime are honoured — the summary is a census of the
	// window, so narrowing it by app or status would defeat its purpose.
	GetAppSummaries(filter models.TaskFilter) ([]models.AppSummary, error)
	// GetTask and SetTaskStatus return ErrTaskNotFound when no task matches id.
	GetTask(id string) (*models.Task, error)
	SetTaskStatus(id, status, reason string) error
	// SupersedeAndAdd cancels the in-progress tasks the new one supersedes and stores
	// it atomically, returning it with the number cancelled. It supersedes a task of
	// the same app sharing an image name (tags ignored) that is no more credentialed
	// than itself (issue #353); the atomicity stops racing submissions both surviving.
	SupersedeAndAdd(task models.Task, reason string) (*models.Task, int64, error)
	Check() bool
	ProcessObsoleteTasks(retryTimes uint)

	// ClaimTask records this instance as the one monitoring the task, for as long
	// as it keeps renewing the claim.
	ClaimTask(id string) error
	// RenewLease extends this instance's claim and reports whether it still holds
	// it. A false return means another replica took the task over, and the caller
	// must stop without writing a status.
	RenewLease(id string) (bool, error)
	// ReleaseOwnedLeases expires every claim this instance holds, so the tasks it
	// was monitoring are taken over immediately instead of after the lease lapses,
	// and reports how many were given up.
	ReleaseOwnedLeases() (int64, error)
	// ClaimExpiredTasks takes over up to limit in-progress tasks whose lease has
	// lapsed, and returns them ready to be monitored again. The returned tasks
	// carry the authority and overrides the rollout acts on, which the API-facing
	// tasks deliberately do not.
	ClaimExpiredTasks(limit int) ([]models.Task, error)
}

// NewState returns a task repository for the configured StateType, already connected.
func NewState(serverConfig *config.ServerConfig) (TaskRepository, error) {
	slog.Debug("Initializing argo-watcher state...")
	var state TaskRepository
	switch name := serverConfig.StateType; name {
	case "postgres":
		slog.Debug("Created postgres state..")
		state = &PostgresState{}
	case "in-memory":
		slog.Debug("Created in-memory state..")
		state = &InMemoryState{}
	default:
		return nil, fmt.Errorf("unexpected state type received: %s", name)
	}

	err := state.Connect(serverConfig)
	if err != nil {
		return nil, err
	}

	return state, nil
}
