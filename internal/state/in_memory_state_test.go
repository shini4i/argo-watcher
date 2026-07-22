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

// createTestTask creates a task with default test values.
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

// taskWithImage returns an in-progress task for the given app deploying a single
// named image, used to exercise image-aware cancellation.
func taskWithImage(app, image string) models.Task {
	task := createTestTask(app)
	task.Images = []models.Image{{Image: image, Tag: "v0.0.1"}}
	return task
}

// TestImageNamesOverlap locks the boundary contract of the overlap helper:
// empty/nil inputs never match, tags are ignored, and a single shared image
// name (even within larger disjoint sets) counts as an overlap.
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

// TestInMemoryState_AddTask verifies that tasks can be added to the in-memory state
// and receive unique IDs.
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

// TestInMemoryState_GetTask verifies that a task can be retrieved by its ID.
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

// TestInMemoryState_GetTask_NotFound verifies that GetTask returns an error
// when the task ID does not exist.
func TestInMemoryState_GetTask_NotFound(t *testing.T) {
	state := InMemoryState{}
	task, err := state.GetTask("non-existent-id")
	assert.Nil(t, task)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

// TestInMemoryState_GetTasks verifies that tasks can be retrieved within a time range
// and optionally filtered by app name.
func TestInMemoryState_GetTasks(t *testing.T) {
	state := InMemoryState{}

	firstTask, err := state.AddTask(createTestTask("Test"))
	require.NoError(t, err)

	secondTask, err := state.AddTask(createTestTask("Test2"))
	require.NoError(t, err)

	now := float64(time.Now().Unix())

	t.Run("returns all tasks within time range", func(t *testing.T) {
		tasks, total := state.GetTasks(now-10, now+10, "", "", 0, 0)
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(2), total)
		// Verify both tasks are present (order may vary when timestamps are equal)
		taskIDs := []string{tasks[0].Id, tasks[1].Id}
		assert.Contains(t, taskIDs, firstTask.Id)
		assert.Contains(t, taskIDs, secondTask.Id)
	})

	t.Run("filters by app name", func(t *testing.T) {
		tasks, total := state.GetTasks(now-10, now+10, "Test", "", 0, 0)
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, firstTask.Id, tasks[0].Id)
	})

	t.Run("returns empty for non-matching app", func(t *testing.T) {
		tasks, total := state.GetTasks(now-10, now+10, "NonExistent", "", 0, 0)
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("filters by status", func(t *testing.T) {
		tasks, total := state.GetTasks(now-10, now+10, "", models.StatusInProgressMessage, 0, 0)
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(2), total)
	})

	t.Run("returns empty for non-matching status", func(t *testing.T) {
		tasks, total := state.GetTasks(now-10, now+10, "", "deployed", 0, 0)
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})
}

// TestInMemoryState_GetTasks_EdgeCases verifies edge cases in GetTasks including
// empty state, pagination, and negative parameters.
func TestInMemoryState_GetTasks_EdgeCases(t *testing.T) {
	t.Run("empty state returns empty slice", func(t *testing.T) {
		state := InMemoryState{}
		tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 0, 0)
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("offset beyond length returns empty slice with total", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 0, 100)
		assert.Empty(t, tasks)
		assert.Equal(t, int64(1), total)
	})

	t.Run("limit restricts returned tasks", func(t *testing.T) {
		state := InMemoryState{}
		for i := 0; i < 5; i++ {
			_, err := state.AddTask(createTestTask("test"))
			require.NoError(t, err)
		}

		tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 2, 0)
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(5), total)
	})

	t.Run("pagination with limit and offset", func(t *testing.T) {
		state := InMemoryState{}
		for i := 0; i < 5; i++ {
			_, err := state.AddTask(createTestTask("test"))
			require.NoError(t, err)
		}

		tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 2, 2)
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(5), total)
	})

	t.Run("negative limit treated as zero (returns all)", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", -5, 0)
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
	})

	t.Run("negative offset treated as zero", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 0, -5)
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
	})
}

// TestInMemoryState_SetTaskStatus verifies that a task's status can be updated.
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

// TestInMemoryState_SetTaskStatus_NotFound verifies that SetTaskStatus returns an error
// when the task ID does not exist.
func TestInMemoryState_SetTaskStatus_NotFound(t *testing.T) {
	state := InMemoryState{}
	err := state.SetTaskStatus("non-existent-id", models.StatusDeployedMessage, "")
	assert.Error(t, err)
	assert.Equal(t, "task not found", err.Error())
}

// TestInMemoryState_CancelInProgressTasks verifies that only in-progress tasks
// for the target app that share an image name with the new deployment are
// switched to cancelled, and that same-app-different-image tasks, already-
// finished tasks, and tasks for other apps are left untouched.
func TestInMemoryState_CancelInProgressTasks(t *testing.T) {
	state := InMemoryState{}

	inProgress, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)

	// Same app, but a different image deployed independently.
	sameAppOtherImage, err := state.AddTask(taskWithImage("app-a", "image-b"))
	require.NoError(t, err)

	otherApp, err := state.AddTask(taskWithImage("app-b", "image-a"))
	require.NoError(t, err)

	finished, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)
	require.NoError(t, state.SetTaskStatus(finished.Id, models.StatusDeployedMessage, ""))

	// A new deployment of image-a for app-a supersedes only the in-progress
	// image-a task.
	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the in-progress app-a task sharing image-a should be cancelled")

	got, err := state.GetTask(inProgress.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, got.Status)
	assert.Equal(t, "superseded", got.StatusReason)

	// Same app but a different image is untouched.
	gotSameApp, err := state.GetTask(sameAppOtherImage.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotSameApp.Status)

	// A different app is untouched.
	gotOther, err := state.GetTask(otherApp.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotOther.Status)

	// An already-deployed task is untouched.
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

	// New deployment shares only image-b with the first task and nothing with the second.
	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-b", Tag: "v2"}, {Image: "image-e", Tag: "v1"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the task sharing an image name should be cancelled")

	gotOverlapping, err := state.GetTask(overlappingTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotOverlapping.Status)

	gotDisjoint, err := state.GetTask(disjointTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotDisjoint.Status)
}

// TestInMemoryState_CancelInProgressTasks_Count verifies the returned count:
// every matching in-progress task is cancelled (aggregation), and a deployment
// that overlaps nothing in flight cancels nothing.
func TestInMemoryState_CancelInProgressTasks_Count(t *testing.T) {
	state := InMemoryState{}

	first, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)
	second, err := state.AddTask(taskWithImage("app-a", "image-a"))
	require.NoError(t, err)

	// No overlap: nothing is cancelled and both tasks stay in progress.
	count, err := state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-z", Tag: "v1"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "a deployment sharing no image should cancel nothing")

	// Overlap: both in-progress tasks sharing image-a are cancelled.
	count, err = state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "every matching in-progress task must be cancelled")

	gotFirst, err := state.GetTask(first.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotFirst.Status)
	gotSecond, err := state.GetTask(second.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotSecond.Status)
}

// TestInMemoryState_ProcessObsoleteTasks verifies that stale in-progress tasks
// are marked as aborted after the threshold period.
func TestInMemoryState_ProcessObsoleteTasks(t *testing.T) {
	state := InMemoryState{}

	// Add a fresh task
	freshTask, err := state.AddTask(createTestTask("Fresh"))
	require.NoError(t, err)

	// Add a stale task by directly manipulating internal state
	staleTask, err := state.AddTask(createTestTask("Stale"))
	require.NoError(t, err)

	// Make the stale task appear old by adjusting its Updated timestamp
	state.mu.Lock()
	for idx := range state.tasks {
		if state.tasks[idx].Id == staleTask.Id {
			state.tasks[idx].Updated = float64(time.Now().Unix()) - TaskStaleThresholdSeconds - 1
		}
	}
	state.mu.Unlock()

	// Run processing with 1 attempt (will complete immediately)
	state.ProcessObsoleteTasks(1)

	// Verify the fresh task is unchanged
	retrievedFresh, err := state.GetTask(freshTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, retrievedFresh.Status)

	// Verify the stale task was marked as aborted with the stale-task reason
	retrievedStale, err := state.GetTask(staleTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusAborted, retrievedStale.Status)
	assert.Equal(t, StaleTaskAbortReason, retrievedStale.StatusReason)
}

// TestInMemoryState_ProcessObsoleteTasks_RemovesAppNotFound verifies that tasks with
// "app not found" status are removed during obsolete task processing.
func TestInMemoryState_ProcessObsoleteTasks_RemovesAppNotFound(t *testing.T) {
	state := InMemoryState{}

	// Add a normal task
	normalTask, err := state.AddTask(createTestTask("Normal"))
	require.NoError(t, err)

	// Add a task and set it to "app not found"
	appNotFoundTask, err := state.AddTask(createTestTask("AppNotFound"))
	require.NoError(t, err)
	err = state.SetTaskStatus(appNotFoundTask.Id, models.StatusAppNotFoundMessage, "")
	require.NoError(t, err)

	// Run processing
	state.ProcessObsoleteTasks(1)

	// Verify normal task still exists
	_, err = state.GetTask(normalTask.Id)
	assert.NoError(t, err)

	// Verify "app not found" task was removed
	_, err = state.GetTask(appNotFoundTask.Id)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

// TestInMemoryState_Check verifies that the Check method returns true for in-memory state.
func TestInMemoryState_Check(t *testing.T) {
	state := InMemoryState{}
	assert.True(t, state.Check())
}

// TestInMemoryState_Connect verifies that Connect method returns nil error.
func TestInMemoryState_Connect(t *testing.T) {
	state := InMemoryState{}
	err := state.Connect(nil)
	assert.NoError(t, err)
}

// TestInMemoryState_ConcurrentAccess verifies thread safety of the in-memory state
// by exercising concurrent reads and writes.
func TestInMemoryState_ConcurrentAccess(t *testing.T) {
	state := InMemoryState{}
	var wg sync.WaitGroup
	taskCount := 50
	errCh := make(chan error, taskCount)

	// Spawn goroutines to add tasks concurrently
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

	// Spawn goroutines to read tasks concurrently
	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 0, 0)
		}()
	}

	wg.Wait()
	close(errCh)

	// Check for any errors from goroutines
	for err := range errCh {
		t.Errorf("AddTask failed: %v", err)
	}

	// Verify all tasks were added
	tasks, total := state.GetTasks(0, float64(time.Now().Unix())+10, "", "", 0, 0)
	assert.Equal(t, int64(taskCount), total)
	assert.Len(t, tasks, taskCount)
}
