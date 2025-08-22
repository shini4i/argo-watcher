package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	state        = InMemoryState{}
	firstTaskId  string
	secondTaskId string
	tasks        = []models.Task{
		{
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
	firstTask, err := state.Add(tasks[0])
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	firstTaskId = firstTask.Id

	secondTask, err := state.Add(tasks[1])
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	secondTaskId = secondTask.Id
}

func TestInMemoryState_GetTask(t *testing.T) {
	task, _ := state.GetTask(firstTaskId)

	assert.NotNil(t, task)
	assert.Equal(t, models.StatusInProgressMessage, task.Status)
}

func TestInMemoryState_GetTasks(t *testing.T) {
	currentTasks := state.GetTasks(float64(time.Now().Unix())-10, float64(time.Now().Unix()), "")
	currentFilteredTasks := state.GetTasks(float64(time.Now().Unix())-10, float64(time.Now().Unix()), "Test")

	assert.Len(t, currentTasks, 2)
	assert.Equal(t, []string{firstTaskId, secondTaskId}, []string{currentTasks[0].Id, currentTasks[1].Id})
	assert.Len(t, currentFilteredTasks, 1)
	assert.Equal(t, []string{firstTaskId}, []string{currentFilteredTasks[0].Id})

}

func TestInMemoryState_SetTaskStatus(t *testing.T) {
	err := state.SetTaskStatus(firstTaskId, models.StatusDeployedMessage, "")
	assert.NoError(t, err)

	taskInfo, _ := state.GetTask(firstTaskId)
	assert.Equal(t, models.StatusDeployedMessage, taskInfo.Status)
}

func TestInMemoryState_ProcessObsoleteTasks(t *testing.T) {
	// update task update time
	state.tasks[1].Updated = state.tasks[1].Updated - 3601

	// run processing
	state.ProcessObsoleteTasks(1)

	// Call the function under test
	tasks = state.GetTasks(float64(time.Now().Unix())-60, float64(time.Now().Unix()), "")

	// Assert the expected results
	assert.Len(t, tasks, 2) // Only non-obsolete tasks should remain

	// Check that the status of the obsolete task has been updated
	assert.Equal(t, models.StatusAborted, tasks[1].Status)
}

func TestInMemoryState_Check(t *testing.T) {
	// semi-useless test, but it's here for completeness
	assert.True(t, state.Check())
}
