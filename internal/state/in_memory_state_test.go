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
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(2), total)
		// Both tasks are present, newest first.
		taskIDs := []string{tasks[0].Id, tasks[1].Id}
		assert.Contains(t, taskIDs, firstTask.Id)
		assert.Contains(t, taskIDs, secondTask.Id)
	})

	t.Run("filters by app name", func(t *testing.T) {
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, App: "Test"})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, firstTask.Id, tasks[0].Id)
	})

	t.Run("returns empty for non-matching app", func(t *testing.T) {
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, App: "NonExistent"})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("filters by status", func(t *testing.T) {
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Status: models.StatusInProgressMessage})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(2), total)
	})

	t.Run("returns empty for non-matching status", func(t *testing.T) {
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Status: "deployed"})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("search matches an app name substring, case-insensitively", func(t *testing.T) {
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "test2"})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, secondTask.Id, tasks[0].Id)
	})

	t.Run("search matches the author and the image tag", func(t *testing.T) {
		_, byAuthor, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "author"})
		assert.Equal(t, int64(2), byAuthor)

		_, byTag, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "test:v0.0.1"})
		assert.Equal(t, int64(2), byTag)
	})

	t.Run("search combines with the app filter", func(t *testing.T) {
		filter := models.TaskFilter{StartTime: now - 10, EndTime: now + 10, App: "Test", Search: "Test2"}
		tasks, total, _ := state.GetTasks(filter)
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("returns empty for a non-matching search", func(t *testing.T) {
		tasks, total, _ := state.GetTasks(models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "payments"})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	// The point of a server-side search: the page bounds the rows returned, not
	// the rows searched, and the total counts every match beyond the page.
	t.Run("pagination applies after the search", func(t *testing.T) {
		filter := models.TaskFilter{StartTime: now - 10, EndTime: now + 10, Search: "test", Limit: 1}
		tasks, total, _ := state.GetTasks(filter)
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(2), total)

		filter.Offset = 1
		second, total, _ := state.GetTasks(filter)
		assert.Len(t, second, 1)
		assert.Equal(t, int64(2), total)
		assert.NotEqual(t, tasks[0].Id, second[0].Id)
	})
}

func TestInMemoryState_GetTasks_EdgeCases(t *testing.T) {
	t.Run("empty state returns empty slice", func(t *testing.T) {
		state := InMemoryState{}
		tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(0), total)
	})

	t.Run("offset beyond length returns empty slice with total", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Offset: 100})
		assert.Empty(t, tasks)
		assert.Equal(t, int64(1), total)
	})

	t.Run("limit restricts returned tasks", func(t *testing.T) {
		state := InMemoryState{}
		for i := 0; i < 5; i++ {
			_, err := state.AddTask(createTestTask("test"))
			require.NoError(t, err)
		}

		tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Limit: 2})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(5), total)
	})

	t.Run("pagination with limit and offset", func(t *testing.T) {
		state := InMemoryState{}
		for i := 0; i < 5; i++ {
			_, err := state.AddTask(createTestTask("test"))
			require.NoError(t, err)
		}

		tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Limit: 2, Offset: 2})
		assert.Len(t, tasks, 2)
		assert.Equal(t, int64(5), total)
	})

	t.Run("negative limit treated as zero (returns all)", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Limit: -5})
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(1), total)
	})

	t.Run("negative offset treated as zero", func(t *testing.T) {
		state := InMemoryState{}
		_, err := state.AddTask(createTestTask("test"))
		require.NoError(t, err)

		tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10, Offset: -5})
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
			_, _, _ = state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10})
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("AddTask failed: %v", err)
	}

	tasks, total, _ := state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Unix()) + 10})
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

// detectRollback takes the first deployed task as the current version, so a
// same-second tie has to name the task that really came last. Created holds whole
// seconds in-memory, which makes ties ordinary, and the ids are random uuids that
// say nothing about order — only the insertion position does.
func TestInMemoryState_GetTasksBreaksASameSecondTieByInsertionOrder(t *testing.T) {
	state := &InMemoryState{}

	now := float64(time.Now().Unix())
	for _, id := range []string{"aaaa", "cccc", "bbbb"} {
		state.tasks = append(state.tasks, models.Task{
			Id: id, App: "app-a", Status: models.StatusDeployedMessage, Created: now,
		})
	}

	for range 5 {
		tasks, _, _ := state.GetTasks(models.TaskFilter{
			EndTime: now + 1,
			App:     "app-a",
		})
		require.Len(t, tasks, 3)
		assert.Equal(t, []string{"bbbb", "cccc", "aaaa"}, []string{tasks[0].Id, tasks[1].Id, tasks[2].Id},
			"the task added last in a second must be the one reported as current")
	}
}

// The production shape: ties inside more than one second. Created decides first and
// insertion order only within a second, which a sort applied before the reverse — or
// an unstable one — gets wrong. Reading must also leave the store as it found it.
func TestInMemoryState_GetTasksOrdersByCreatedThenInsertion(t *testing.T) {
	state := &InMemoryState{}

	now := float64(time.Now().Unix())
	state.tasks = append(state.tasks,
		models.Task{Id: "a-early", App: "app-a", Created: now - 1},
		models.Task{Id: "b-late", App: "app-a", Created: now},
		models.Task{Id: "c-early", App: "app-a", Created: now - 1},
		models.Task{Id: "d-late", App: "app-a", Created: now},
	)

	for range 2 {
		tasks, _, _ := state.GetTasks(models.TaskFilter{EndTime: now + 1, App: "app-a"})
		require.Len(t, tasks, 4)
		assert.Equal(t, []string{"d-late", "b-late", "c-early", "a-early"},
			[]string{tasks[0].Id, tasks[1].Id, tasks[2].Id, tasks[3].Id})
	}

	assert.Equal(t, []string{"a-early", "b-late", "c-early", "d-late"},
		[]string{state.tasks[0].Id, state.tasks[1].Id, state.tasks[2].Id, state.tasks[3].Id},
		"reading must not reorder the stored insertion sequence")
}

// Insertion position is the only recency a same-second tie has, so the sweep that
// rewrites the store must not reorder what it keeps.
func TestInMemoryState_ProcessObsoleteTasksKeepsInsertionOrder(t *testing.T) {
	state := &InMemoryState{}

	now := float64(time.Now().Unix())
	stale := now - TaskStaleThresholdSeconds - 60
	state.tasks = append(state.tasks,
		models.Task{Id: "kept-first", App: "app-a", Status: models.StatusDeployedMessage, Created: now, Updated: now},
		models.Task{Id: "dropped", App: "app-a", Status: models.StatusAppNotFoundMessage, Created: now, Updated: now},
		models.Task{Id: "aborted", App: "app-a", Status: models.StatusInProgressMessage, Created: stale, Updated: stale},
		models.Task{Id: "kept-last", App: "app-a", Status: models.StatusDeployedMessage, Created: now, Updated: now},
	)

	state.ProcessObsoleteTasks(1)

	assert.Equal(t, []string{"kept-first", "aborted", "kept-last"},
		[]string{state.tasks[0].Id, state.tasks[1].Id, state.tasks[2].Id})

	tasks, _, _ := state.GetTasks(models.TaskFilter{EndTime: now + 1, App: "app-a"})
	require.NotEmpty(t, tasks)
	assert.Equal(t, "kept-last", tasks[0].Id, "the last task stored in a second stays the current one")
}
