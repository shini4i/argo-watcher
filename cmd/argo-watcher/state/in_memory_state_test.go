package state

import (
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"

	m "github.com/shini4i/argo-watcher/internal/models"
)

const taskId = "9b67e344-e5b5-11ec-bc56-8a68373f0f50"

var (
	state = InMemoryState{}
	tasks = []m.Task{
		{
			Id:      taskId,
			Created: float64(time.Now().Unix()),
			App:     "Test",
			Author:  "Test Author",
			Project: "Test Project",
			Images: []m.Image{
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
			Images: []m.Image{
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

func TestInMemoryState_GetTaskStatus(t *testing.T) {
	status := state.GetTaskStatus(taskId)

	if status != "in progress" {
		t.Errorf("got %s, expected %s", status, "in progress")
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

	if status := state.GetTaskStatus(taskId); status != "deployed" {
		t.Errorf("got %s, expected %s", status, "deployed")
	}
}

func TestInMemoryState_GetAppList(t *testing.T) {
	apps := state.GetAppList()

	if !reflect.DeepEqual(apps, []string{"Test", "Test2"}) {
		t.Errorf("got %s, expected %s", apps, []string{"Test", "Test2"})
	}
}
