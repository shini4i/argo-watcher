package state

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

func createTestTask(app string) models.Task {
	return models.Task{
		App:     app,
		Author:  "Test Author",
		Project: "Test Project",
		Images: []models.Image{
			{
				Image: "test",
				Tag:   "v0.0.1",
			},
		},
		Status: models.StatusInProgressMessage,
	}
}

func taskWithImage(app, image string) models.Task {
	task := createTestTask(app)
	task.Images = []models.Image{{Image: image, Tag: "v0.0.1"}}
	return task
}

// Tags are ignored, and a single shared image name counts as an overlap.
func TestImageNamesOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    []models.Image
		b    []models.Image
		want bool
	}{
		{"both nil", nil, nil, false},
		{"first empty", nil, []models.Image{{Image: "image-a", Tag: "v1"}}, false},
		{"second empty", []models.Image{{Image: "image-a", Tag: "v1"}}, nil, false},
		{"fully disjoint", []models.Image{{Image: "image-a"}}, []models.Image{{Image: "image-b"}}, false},
		{"same name different tags", []models.Image{{Image: "image-a", Tag: "v1"}}, []models.Image{{Image: "image-a", Tag: "v2"}}, true},
		{"partial overlap in larger sets", []models.Image{{Image: "image-a"}, {Image: "image-b"}}, []models.Image{{Image: "image-b"}, {Image: "image-c"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, imageNamesOverlap(tt.a, tt.b))
		})
	}
}

func TestInMemoryState_AddTask(t *testing.T) {
	state := InMemoryState{}

	firstTask, err := state.AddTask(createTestTask("Test"))
	require.NoError(t, err)
	assert.NotEmpty(t, firstTask.Id)
	assert.Equal(t, models.StatusInProgressMessage, firstTask.Status)

	secondTask, err := state.AddTask(createTestTask("Test2"))
	require.NoError(t, err)
	assert.NotEmpty(t, secondTask.Id)
	assert.NotEqual(t, firstTask.Id, secondTask.Id, "Each task should have a unique ID")
}

func TestInMemoryState_GetTask(t *testing.T) {
	state := InMemoryState{}

	addedTask, err := state.AddTask(createTestTask("Test"))
	require.NoError(t, err)

	retrievedTask, err := state.GetTask(addedTask.Id)
	require.NoError(t, err)
	assert.NotNil(t, retrievedTask)
	assert.Equal(t, addedTask.Id, retrievedTask.Id)
	assert.Equal(t, models.StatusInProgressMessage, retrievedTask.Status)
}

func TestInMemoryState_GetTask_NotFound(t *testing.T) {
	state := InMemoryState{}
	task, err := state.GetTask("non-existent-id")
	assert.Nil(t, task)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryState_GetTasks(t *testing.T) {
	state := InMemoryState{}

	firstTask, err := state.AddTask(createTestTask("Test"))
	require.NoError(t, err)

	secondTask, err := state.AddTask(createTestTask("Test2"))
	require.NoError(t, err)

	now := float64(time.Now().Unix())

	t.Run("returns all tasks within time range", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(2), total)
		// Verify both tasks are present (order may vary when timestamps are equal)
		taskIDs := []string{tasks[0].Id, tasks[1].Id}
		assert.Contains(t, taskIDs, firstTask.Id)
		assert.Contains(t, taskIDs, secondTask.Id)
	})

	t.Run("filters by app name", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, App: "Test"})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, firstTask.Id, tasks[0].Id)
	})

	t.Run("returns empty for non-matching app", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, App: "NonExistent"})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("filters by status", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Status: models.StatusInProgressMessage})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(2), total)
	})

	t.Run("returns empty for non-matching status", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Status: "deployed"})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("search matches an app name substring, case-insensitively", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "test2"})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, secondTask.Id, tasks[0].Id)
	})

	t.Run("search matches the author and the image tag", func(t *testing.T) {
		_, byAuthor := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "author"})
		assert.Equal(t, int64(2), byAuthor)

		_, byTag := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "test:v0.0.1"})
		assert.Equal(t, int64(2), byTag)
	})

	t.Run("search combines with the app filter", func(t *testing.T) {
		filter := models.TaskFilter{StartTime: now - 10, EndTime: now + 10, App: "Test", Search: "Test2"}
		tasks, total := state.GetTasks(filter)
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("returns empty for a non-matching search", func(t *testing.T) {
		tasks, total := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "payments"})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	// The point of a server-side search: the page bounds the rows returned, not
	// the rows searched, and the total counts every match beyond the page.
	t.Run("pagination applies after the search", func(t *testing.T) {
		filter := models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "test", Limit: 1}
		tasks, total := state.GetTasks(filter)
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(2), total)

		filter.Offset = 1
		second, total := state.GetTasks(filter)
		assert.Len(t, second, 1)
		assert.Equal(t, int64(2), total)
		assert.NotEqual(t, tasks[0].Id, second[0].Id)
	})
}

func TestInMemoryState_GetTasks_EdgeCases(t *testing.T) {
	t.Run("empty state returns empty slice", func(t *testing.T) {
		state := InMemoryState{}
		tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("offset beyond length returns empty slice with total", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Offset: 100})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(1), total)
	})

	t.Run("limit restricts returned tasks", func(t *testing.T) {
		state := InMemoryState{}
		for i := 0; i < 5; i++ {
			_, err := state.AddTask(createTestTask("test"))
			require.NoError(t, err)
		}

		tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Limit: 2})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(5), total)
	})

	t.Run("pagination with limit and offset", func(t *testing.T) {
		state := InMemoryState{}
		for i := 0; i < 5; i++ {
			_, err := state.AddTask(createTestTask("test"))
			require.NoError(t, err)
		}

		tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Limit: 2, Offset: 2})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(5), total)
	})

	t.Run("negative limit treated as zero (returns all)", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Limit: -5})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
	})

	t.Run("negative offset treated as zero", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Offset: -5})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
	})
}

func TestInMemoryState_SetTaskStatus(t *testing.T) {
	state := InMemoryState{}

	task, err := state.AddTask(createTestTask("Test"))
	require.NoError(t, err)

	err = state.SetTaskStatus(task.Id, models.StatusDeployedMessage, "deployed successfully")
	assert.NoError(t, err)

	updatedTask, err := state.GetTask(task.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusDeployedMessage, updatedTask.Status)
	assert.Equal(t, "deployed successfully", updatedTask.StatusReason)
}

func TestInMemoryState_SetTaskStatus_NotFound(t *testing.T) {
	state := InMemoryState{}
	err := state.SetTaskStatus("non-existent-id", models.StatusDeployedMessage, "")
	assert.Error(t, err)
	assert.Equal(t, "task not found", err.Error())
}

func TestInMemoryState_CancelInProgressTasks(t *testing.T) {
	state := InMemoryState{}

	inProgress, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)

	sameAppOtherImage, err := state.AddTask(taskWithImage("app-a", "image-b"))
	require.NoError(t, err)

	otherApp, err := state.AddTask(taskWithImage("app-b", "image-a"))
	require.NoError(t, err)

	finished, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)
	require.NoError(t, state.SetTaskStatus(finished.Id, models.StatusDeployedMessage, ""))

	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the in-progress app-a task sharing image-a should be cancelled")

	got, err := state.GetTask(inProgress.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, got.Status)
	assert.Equal(t, "superseded", got.StatusReason)

	gotSameApp, err := state.GetTask(sameAppOtherImage.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotSameApp.Status)

	gotOther, err := state.GetTask(otherApp.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotOther.Status)

	gotFinished, err := state.GetTask(finished.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusDeployedMessage, gotFinished.Status)
}

// TestInMemoryState_CancelInProgressTasks_MultiImageOverlap verifies the "any
// shared image name" semantics: a multi-image in-progress task is cancelled when
// the new deployment shares only one of its images, while a task sharing none is
// left alone. This is what distinguishes overlap from set-equality matching.
func TestInMemoryState_CancelInProgressTasks_MultiImageOverlap(t *testing.T) {
	state := InMemoryState{}

	overlapping := createTestTask("app-a")
	overlapping.Images = []models.Image{{Image: "image-a", Tag: "v1"}, {Image: "image-b", Tag: "v1"}}
	overlappingTask, err := state.AddTask(overlapping)
	require.NoError(t, err)

	disjoint := createTestTask("app-a")
	disjoint.Images = []models.Image{{Image: "image-c", Tag: "v1"}, {Image: "image-d", Tag: "v1"}}
	disjointTask, err := state.AddTask(disjoint)
	require.NoError(t, err)

	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-b", Tag: "v2"}, {Image: "image-e", Tag: "v1"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the task sharing an image name should be cancelled")

	gotOverlapping, err := state.GetTask(overlappingTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotOverlapping.Status)

	gotDisjoint, err := state.GetTask(disjointTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotDisjoint.Status)
}

func TestInMemoryState_CancelInProgressTasks_Count(t *testing.T) {
	state := InMemoryState{}

	first, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)
	second, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)

	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-z", Tag: "v1"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "a deployment sharing no image should cancel nothing")

	count, err = state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "every matching in-progress task must be cancelled")

	gotFirst, err := state.GetTask(first.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotFirst.Status)
	gotSecond, err := state.GetTask(second.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotSecond.Status)
}

// TestInMemoryState_CancelInProgressTasks_Authority locks the rule that a task
// may only supersede in-flight work carrying no more authority than itself: an
// uncredentialed (unvalidated) deployment must never cancel a credentialed one.
// That is what stops an anonymous request from aborting a credentialed
// deployment's pending git write-back. Every other combination still supersedes,
// so token-less setups keep behaving exactly as before.
func TestInMemoryState_CancelInProgressTasks_Authority(t *testing.T) {
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
			state := InMemoryState{}

			victim := taskWithImage("app-a", "image-a")
			victim.Validated = tt.victimValidated
			inFlight, err := state.AddTask(victim)
			require.NoError(t, err)

			count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", tt.newTaskValidated)
			require.NoError(t, err)

			got, err := state.GetTask(inFlight.Id)
			require.NoError(t, err)

			if tt.wantCancelled {
				assert.Equal(t, int64(1), count)
				assert.Equal(t, models.StatusCancelledMessage, got.Status)
				return
			}
			assert.Equal(t, int64(0), count)
			assert.Equal(t, models.StatusInProgressMessage, got.Status)
		})
	}
}

// TestInMemoryState_CancelInProgressTasks_AuthorityMixedFleet covers the setup
// that motivates a per-task rule rather than an instance-wide one: a single app
// with both a credentialed and an uncredentialed rollout in flight. An anonymous
// deployment supersedes only the uncredentialed one and leaves the credentialed
// rollout running.
func TestInMemoryState_CancelInProgressTasks_AuthorityMixedFleet(t *testing.T) {
	state := InMemoryState{}

	credentialed := taskWithImage("app-a", "image-a")
	credentialed.Validated = true
	credentialedTask, err := state.AddTask(credentialed)
	require.NoError(t, err)

	anonymousTask, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)

	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the uncredentialed rollout may be superseded")

	gotCredentialed, err := state.GetTask(credentialedTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotCredentialed.Status)

	gotAnonymous, err := state.GetTask(anonymousTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotAnonymous.Status)
}

func TestInMemoryState_ProcessObsoleteTasks(t *testing.T) {
	state := InMemoryState{}

	freshTask, err := state.AddTask(createTestTask("Fresh"))
	require.NoError(t, err)

	staleTask, err := state.AddTask(createTestTask("Stale"))
	require.NoError(t, err)

	state.mu.Lock()
	for idx := range state.tasks {
		if state.tasks[idx].Id == staleTask.Id {
			state.tasks[idx].Updated = float64(time.Now().Unix()) - TaskStaleThresholdSeconds - 1
		}
	}
	state.mu.Unlock()

	// Run processing with 1 attempt (will complete immediately)
	state.ProcessObsoleteTasks(1)

	retrievedFresh, err := state.GetTask(freshTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, retrievedFresh.Status)

	retrievedStale, err := state.GetTask(staleTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusAborted, retrievedStale.Status)
	assert.Equal(t, StaleTaskAbortReason, retrievedStale.StatusReason)
}

func TestInMemoryState_ProcessObsoleteTasks_RemovesAppNotFound(t *testing.T) {
	state := InMemoryState{}

	normalTask, err := state.AddTask(createTestTask("Normal"))
	require.NoError(t, err)

	appNotFoundTask, err := state.AddTask(createTestTask("AppNotFound"))
	require.NoError(t, err)
	err = state.SetTaskStatus(appNotFoundTask.Id, models.StatusAppNotFoundMessage, "")
	require.NoError(t, err)

	state.ProcessObsoleteTasks(1)

	_, err = state.GetTask(normalTask.Id)
	assert.NoError(t, err)

	_, err = state.GetTask(appNotFoundTask.Id)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryState_Check(t *testing.T) {
	state := InMemoryState{}
	assert.True(t, state.Check())
}

func TestInMemoryState_Connect(t *testing.T) {
	state := InMemoryState{}
	err := state.Connect(nil)
	assert.NoError(t, err)
}

func TestInMemoryState_ConcurrentAccess(t *testing.T) {
	state := InMemoryState{}
	var wg sync.WaitGroup
	taskCount := 50
	errCh := make(chan error, taskCount)

	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := state.AddTask(createTestTask(fmt.Sprintf("App%d", i)))
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10})
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("AddTask failed: %v", err)
	}

	tasks, total := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10})
	assert.Equal(t, int64(taskCount), total)
	assert.Len(t, tasks, taskCount)
}

// TestInMemoryState_ProcessObsoleteTasksSparesALongerTimeout pins that the sweep
// gives up only on a task that could not still be running: a rollout whose own
// window is longer than the staleness threshold is being monitored right now, and
// the monitor stops polling as soon as it reads "aborted" (issue #562).
func TestInMemoryState_ProcessObsoleteTasksSparesALongerTimeout(t *testing.T) {
	state := InMemoryState{}

	longTask := createTestTask("LongRollout")
	longTask.Timeout = TaskStaleThresholdSeconds * 2
	stored, err := state.AddTask(longTask)
	require.NoError(t, err)

	state.mu.Lock()
	for idx := range state.tasks {
		if state.tasks[idx].Id == stored.Id {
			state.tasks[idx].Updated = float64(time.Now().Unix()) - TaskStaleThresholdSeconds - 1
		}
	}
	state.mu.Unlock()

	state.ProcessObsoleteTasks(1)

	got, err := state.GetTask(stored.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, got.Status)

	// Past its own window the safety net still fires, or a monitor that vanished
	// would leave the task in progress forever.
	state.mu.Lock()
	for idx := range state.tasks {
		if state.tasks[idx].Id == stored.Id {
			state.tasks[idx].Updated = float64(time.Now().Unix()) - float64(longTask.Timeout) - TaskStaleThresholdSeconds - 1
		}
	}
	state.mu.Unlock()

	state.ProcessObsoleteTasks(1)

	got, err = state.GetTask(stored.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusAborted, got.Status)
}
