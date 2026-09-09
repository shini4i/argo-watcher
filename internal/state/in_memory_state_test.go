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

func TestInMemoryState_Contract(t *testing.T) {
	runTaskRepositoryContract(t, func(t *testing.T) TaskRepository {
		return &InMemoryState{}
	})
}
