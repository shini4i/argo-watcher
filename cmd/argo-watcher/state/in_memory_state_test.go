package state

import (
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
	"time"

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
			Status: "in progress",
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
			Status: "in progress",
		},
	}
)

func TestInMemoryState_Add(t *testing.T) {
	for _, task := range tasks {
		state.Add(task)
	}
}

func TestInMemoryState_GetTask(t *testing.T) {
	task, _ := state.GetTask(taskId)

	if task.Status != "in progress" {
		t.Errorf("got %s, expected %s", task.Status, "in progress")
	}
}

func TestInMemoryState_GetTasks(t *testing.T) {
	currentTasks := state.GetTasks(float64(time.Now().Unix())-10, float64(time.Now().Unix()), "")
	currentFilteredTasks := state.GetTasks(float64(time.Now().Unix())-10, float64(time.Now().Unix()), "Test")

	if !reflect.DeepEqual(tasks, currentTasks) {
		t.Errorf("got %v, expected %v", currentTasks, tasks)
	}

	if l := len(currentFilteredTasks); l != 1 {
		t.Errorf("got %d tasks, expected %d", l, 1)
	}
}

func TestInMemoryState_SetTaskStatus(t *testing.T) {
	state.SetTaskStatus(taskId, "deployed", "")

	if taskInfo, _ := state.GetTask(taskId); taskInfo.Status != "deployed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "deployed")
	}
}

func TestInMemoryState_GetAppList(t *testing.T) {
	apps := state.GetAppList()

	if !reflect.DeepEqual(apps, []string{"Test", "Test2"}) {
		t.Errorf("got %s, expected %s", apps, []string{"Test", "Test2"})
	}
}

func TestProcessObsoleteTasks(t *testing.T) {
	tasks := []models.Task{
		{
			Id:      "1",
			Created: float64(time.Now().Unix()),
			Updated: float64(time.Now().Unix()),
			Images:  []models.Image{{Image: "image1", Tag: "tag1"}},
			Status:  "app not found",
		},
		{
			Id:      "2",
			Created: float64(time.Now().Unix()),
			Updated: float64(time.Now().Unix() - 7200), // Older than 1 hour
			Images:  []models.Image{{Image: "image2", Tag: "tag2"}},
			Status:  "in progress",
		},
		{
			Id:      "3",
			Created: float64(time.Now().Unix()),
			Updated: float64(time.Now().Unix()),
			Images:  []models.Image{{Image: "image3", Tag: "tag3"}},
			Status:  "deployed",
		},
	}

	// Call the function under test
	tasks = processInMemoryObsoleteTasks(tasks)

	// Assert the expected results
	assert.Len(t, tasks, 2) // Only non-obsolete tasks should remain

	// Check that the status of the obsolete task has been updated
	assert.Equal(t, "aborted", tasks[0].Status)
}
