package state

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

var errDesiredRetry = errors.New("desired retry error")

// ErrTaskNotFound is returned by TaskRepository.GetTask when no task exists for
// the requested id. Callers use errors.Is to distinguish a genuine "not found"
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
	AddTask(task models.Task) (*models.Task, error)
	GetTasks(startTime float64, endTime float64, app string, status string, limit int, offset int) ([]models.Task, int64)
	GetTask(id string) (*models.Task, error)
	SetTaskStatus(id, status, reason string) error
	// CancelInProgressTasks marks in-progress tasks for the given app as
	// cancelled and returns how many were affected. A task is only cancelled when
	// it shares at least one image name with the supplied images, so independent
	// per-image deployments of the same app do not cancel each other (issue #353).
	// Tags are ignored on purpose: a newer tag of the same image must still
	// supersede the older in-flight rollout. Operating on the shared state makes
	// the cancellation visible to every replica, not just the one handling the new
	// deployment.
	//
	// newTaskValidated is the superseding deployment's own authority: an
	// uncredentialed task never cancels a credentialed one, which would otherwise
	// let an anonymous request abort a credentialed rollout's git write-back.
	CancelInProgressTasks(app string, images []models.Image, reason string, newTaskValidated bool) (int64, error)
	Check() bool
	ProcessObsoleteTasks(retryTimes uint)
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
