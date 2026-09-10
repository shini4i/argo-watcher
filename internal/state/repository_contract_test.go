package state

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// contractTask builds the minimal task the contract cases submit: one app, one image.
func contractTask(app, image string) models.Task {
	return models.Task{
		App:     app,
		Author:  "Test Author",
		Project: "Test Project",
		Images:  []models.Image{{Image: image, Tag: "v0.0.1"}},
	}
}

// runTaskRepositoryContract drives every behaviour both TaskRepository implementations
// must share, each case on a fresh repository from newRepository. Backend-specific
// behaviour (lease timing, staleness windows, retention) stays in the per-backend files.
func runTaskRepositoryContract(t *testing.T, newRepository func(t *testing.T) TaskRepository) {
	t.Helper()

	t.Run("AddTask stamps id, status and both timestamps in seconds", func(t *testing.T) {
		repository := newRepository(t)

		// Poisoned so the assertions pin the overwrite, not just that a value is non-zero.
		const farFuture = 4102444800
		submitted := contractTask("app-a", "image-a")
		submitted.Created = farFuture
		submitted.Updated = farFuture

		added, err := repository.AddTask(submitted)
		require.NoError(t, err)

		now := float64(time.Now().Unix())
		assert.NotEmpty(t, added.Id)
		assert.Equal(t, models.StatusInProgressMessage, added.Status)
		assert.InDelta(t, now, added.Created, 5, "Created must be a Unix timestamp in seconds")
		assert.InDelta(t, now, added.Updated, 5, "Updated must be a Unix timestamp in seconds")
		assert.Equal(t, added.Created, added.Updated, "a freshly created task has not been updated since")

		second, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)
		assert.NotEqual(t, added.Id, second.Id, "each task gets its own id")
	})

	t.Run("GetTask returns the task AddTask reported, timestamps included", func(t *testing.T) {
		repository := newRepository(t)

		added, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)

		got, err := repository.GetTask(added.Id)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, added.Id, got.Id)
		assert.Equal(t, "app-a", got.App)
		assert.Equal(t, models.StatusInProgressMessage, got.Status)
		assert.InDelta(t, added.Created, got.Created, 1, "AddTask and GetTask must report Created in the same unit")
		assert.InDelta(t, added.Updated, got.Updated, 1, "AddTask and GetTask must report Updated in the same unit")
	})

	t.Run("GetTask on an unknown id is ErrTaskNotFound", func(t *testing.T) {
		repository := newRepository(t)

		got, err := repository.GetTask(uuid.NewString())
		assert.Nil(t, got)
		assert.ErrorIs(t, err, ErrTaskNotFound)
	})

	t.Run("SetTaskStatus updates status, reason and the Updated stamp", func(t *testing.T) {
		repository := newRepository(t)

		added, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)

		require.NoError(t, repository.SetTaskStatus(added.Id, models.StatusDeployedMessage, "finished"))

		got, err := repository.GetTask(added.Id)
		require.NoError(t, err)
		assert.Equal(t, models.StatusDeployedMessage, got.Status)
		assert.Equal(t, "finished", got.StatusReason)
		// The UI derives a deployment's duration from Updated-Created, so a status
		// write must move Updated (both stamps may still share one wall-clock second).
		assert.InDelta(t, float64(time.Now().Unix()), got.Updated, 5, "Updated must be re-stamped in Unix seconds")
		assert.GreaterOrEqual(t, got.Updated, added.Created)
	})

	t.Run("SetTaskStatus on an unknown id is ErrTaskNotFound", func(t *testing.T) {
		repository := newRepository(t)

		err := repository.SetTaskStatus(uuid.NewString(), models.StatusDeployedMessage, "")
		assert.ErrorIs(t, err, ErrTaskNotFound)
	})

	// Tags are ignored, and a single shared image name counts as an overlap.
	t.Run("CancelInProgressTasks cancels only in-progress tasks of the app sharing an image name", func(t *testing.T) {
		repository := newRepository(t)

		inProgress, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)
		sameAppOtherImage, err := repository.AddTask(contractTask("app-a", "image-b"))
		require.NoError(t, err)
		otherApp, err := repository.AddTask(contractTask("app-b", "image-a"))
		require.NoError(t, err)
		finished, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)
		require.NoError(t, repository.SetTaskStatus(finished.Id, models.StatusDeployedMessage, ""))

		count, err := repository.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "only the in-progress app-a task sharing image-a should be cancelled")

		assertStatus(t, repository, inProgress.Id, models.StatusCancelledMessage)
		got, err := repository.GetTask(inProgress.Id)
		require.NoError(t, err)
		assert.Equal(t, "superseded", got.StatusReason)

		assertStatus(t, repository, sameAppOtherImage.Id, models.StatusInProgressMessage)
		assertStatus(t, repository, otherApp.Id, models.StatusInProgressMessage)
		assertStatus(t, repository, finished.Id, models.StatusDeployedMessage)
	})

	// Overlap, not set equality: sharing one of several images is enough.
	t.Run("CancelInProgressTasks matches on any shared image name", func(t *testing.T) {
		repository := newRepository(t)

		overlapping := contractTask("app-a", "image-a")
		overlapping.Images = []models.Image{{Image: "image-a", Tag: "v1"}, {Image: "image-b", Tag: "v1"}}
		overlappingTask, err := repository.AddTask(overlapping)
		require.NoError(t, err)

		disjoint := contractTask("app-a", "image-c")
		disjoint.Images = []models.Image{{Image: "image-c", Tag: "v1"}, {Image: "image-d", Tag: "v1"}}
		disjointTask, err := repository.AddTask(disjoint)
		require.NoError(t, err)

		count, err := repository.CancelInProgressTasks("app-a", []models.Image{{Image: "image-b", Tag: "v2"}, {Image: "image-e", Tag: "v1"}}, "superseded", false)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "only the task sharing an image name should be cancelled")

		assertStatus(t, repository, overlappingTask.Id, models.StatusCancelledMessage)
		assertStatus(t, repository, disjointTask.Id, models.StatusInProgressMessage)
	})

	t.Run("CancelInProgressTasks reports how many tasks it cancelled", func(t *testing.T) {
		repository := newRepository(t)

		first, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)
		second, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)

		count, err := repository.CancelInProgressTasks("app-a", []models.Image{{Image: "image-z", Tag: "v1"}}, "superseded", false)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count, "a deployment sharing no image should cancel nothing")

		count, err = repository.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count, "every matching in-progress task must be cancelled")

		assertStatus(t, repository, first.Id, models.StatusCancelledMessage)
		assertStatus(t, repository, second.Id, models.StatusCancelledMessage)
	})

	// An uncredentialed deployment must never cancel a credentialed one: that would let an
	// anonymous request abort a credentialed rollout's pending git write-back. Every other
	// combination supersedes, so token-less setups keep behaving as before.
	t.Run("CancelInProgressTasks honours the authority rule", func(t *testing.T) {
		tests := []struct {
			name             string
			victimValidated  bool
			newTaskValidated bool
			wantCancelled    bool
		}{
			{"unvalidated must not cancel validated", true, false, false},
			{"validated cancels validated", true, true, true},
			{"unvalidated cancels unvalidated", false, false, true},
			{"validated cancels unvalidated", false, true, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				repository := newRepository(t)

				victim := contractTask("app-a", "image-a")
				victim.Validated = tt.victimValidated
				inFlight, err := repository.AddTask(victim)
				require.NoError(t, err)

				count, err := repository.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", tt.newTaskValidated)
				require.NoError(t, err)

				if tt.wantCancelled {
					assert.Equal(t, int64(1), count)
					assertStatus(t, repository, inFlight.Id, models.StatusCancelledMessage)
					return
				}
				assert.Equal(t, int64(0), count)
				assertStatus(t, repository, inFlight.Id, models.StatusInProgressMessage)
			})
		}
	})

	// One app with a credentialed and an uncredentialed rollout in flight: an anonymous
	// deployment supersedes only the uncredentialed one. This is why the rule is per task.
	t.Run("CancelInProgressTasks leaves the credentialed rollout of a mixed fleet running", func(t *testing.T) {
		repository := newRepository(t)

		credentialed := contractTask("app-a", "image-a")
		credentialed.Validated = true
		credentialedTask, err := repository.AddTask(credentialed)
		require.NoError(t, err)

		anonymousTask, err := repository.AddTask(contractTask("app-a", "image-a"))
		require.NoError(t, err)

		count, err := repository.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "only the uncredentialed rollout may be superseded")

		assertStatus(t, repository, credentialedTask.Id, models.StatusInProgressMessage)
		assertStatus(t, repository, anonymousTask.Id, models.StatusCancelledMessage)
	})
}

// assertStatus reads the task back and checks its status.
func assertStatus(t *testing.T, repository TaskRepository, id, want string) {
	t.Helper()
	got, err := repository.GetTask(id)
	require.NoError(t, err)
	assert.Equal(t, want, got.Status)
}
