package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/google/uuid"

	"github.com/shini4i/argo-watcher/internal/models"
)

const taskId = "9b67e344-e5b5-11ec-bc56-8a68373f0f50"

var (
	state = InMemoryState{}
	tasks = []models.Task{
		{
			Id:      taskId,
			Created: float64(time.Now().Unix()),
			App:     "Test",
			Author:  "Test Author",
			Project: "Test Project",
			Images: []models.Image{
				{
					Image: "test",
					Tag:   "v0.0.1",
				},
			},
			Status: models.StatusInProgressMessage,
		},
		{
			Id:      uuid.New().String(),
			Created: float64(time.Now().Unix()),
			App:     "Test2",
			Author:  "Test Author",
			Project: "Test Project",
			Images: []models.Image{
				{
					Image: "test2",
					Tag:   "v0.0.1",
				},
			},
			Status: models.StatusInProgressMessage,
		},
	}
)

func TestInMemoryState_Add(t *testing.T) {
	for _, task := range tasks {
		if _, err := state.Add(task); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}
}

func TestInMemoryState_GetTask(t *testing.T) {
	task, _ := state.GetTask(taskId)

	assert.Equal(t, task.Status, models.StatusInProgressMessage)
}

func TestInMemoryState_GetTasks(t *testing.T) {
	currentTasks := state.GetTasks(float64(time.Now().Unix())-10, float64(time.Now().Unix()), "")
	currentFilteredTasks := state.GetTasks(float64(time.Now().Unix())-10, float64(time.Now().Unix()), "Test")

	assert.Equal(t, tasks, currentTasks)
	assert.Len(t, currentFilteredTasks, 1)
}

func TestInMemoryState_SetTaskStatus(t *testing.T) {
	state.SetTaskStatus(taskId, "deployed", "")

	if taskInfo, _ := state.GetTask(taskId); taskInfo.Status != "deployed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "deployed")
	}
}

func TestInMemoryState_GetAppList(t *testing.T) {
	assert.Equal(t, state.GetAppList(), []string{"Test", "Test2"})
}

func TestInMemoryState_GetAppListEmpty(t *testing.T) {
	state := InMemoryState{}
	// We must make sure that we are returning an empty slice and not nil
	assert.Equal(t, state.GetAppList(), []string{})
}

func TestInMemoryState_ProcessObsoleteTasks(t *testing.T) {
	state.ProcessObsoleteTasks(1)

	// Call the function under test
	tasks = state.GetTasks(float64(time.Now().Unix())-60, float64(time.Now().Unix()), "")

	// Assert the expected results
	assert.Len(t, tasks, 2) // Only non-obsolete tasks should remain

	// Check that the status of the obsolete task has been updated
	assert.Equal(t, "aborted", tasks[1].Status)
}

func TestInMemoryState_Check(t *testing.T) {
	// semi-useless test, but it's here for completeness
	assert.True(t, state.Check())
}
