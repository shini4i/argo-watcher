package state

import (
	"github.com/google/uuid"
	"os"
	"reflect"
	"testing"
	"time"
	m "watcher/models"
)

const postgresTaskId = "782e6e84-e67d-11ec-9f2f-8a68373f0f50"

var (
	created       = float64(time.Now().Unix())
	postgresState = PostgresState{}
	postgresTasks = []m.Task{
		{
			Id:      postgresTaskId,
			Created: created,
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
			Created: created,
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

func TestPostgresState_Add(t *testing.T) {
	err := os.Setenv("DB_MIGRATIONS_PATH", "../../db/migrations")
	if err != nil {
		panic(err)
	}
	postgresState.Connect()
	_, err = postgresState.db.Exec("TRUNCATE TABLE tasks")
	if err != nil {
		panic(err)
	}
	for _, task := range postgresTasks {
		postgresState.Add(task)
	}
}

func TestPostgresState_GetTaskStatus(t *testing.T) {
	status := postgresState.GetTaskStatus(postgresTaskId)

	if status != "in progress" {
		t.Errorf("got %s, expected %s", status, "in progress")
	}
}

func TestPostgresState_GetTasks(t *testing.T) {
	currentTasks := postgresState.GetTasks(float64(time.Now().Unix()-5*60*1000), float64(time.Now().Unix()), "")
	currentFilteredTasks := postgresState.GetTasks(float64(time.Now().Unix()-5*60*1000), float64(time.Now().Unix()), "Test")

	// A much simpler check. Need to reconsider this in the future.
	if len(currentTasks) != len(postgresTasks) {
		t.Errorf("Unfiltered tasks count does not match. Got %d, expected %d", len(currentTasks), len(postgresTasks))
	}

	if l := len(currentFilteredTasks); l != 1 {
		t.Errorf("Filtered tasks count does not match. Got %d tasks, expected %d", l, 1)
	}
}

func TestPostgresState_SetTaskStatus(t *testing.T) {
	postgresState.SetTaskStatus(postgresTaskId, "deployed")

	if status := postgresState.GetTaskStatus(postgresTaskId); status != "deployed" {
		t.Errorf("got %s, expected %s", status, "deployed")
	}
}

func TestPostgresState_GetAppList(t *testing.T) {
	apps := postgresState.GetAppList()

	if !reflect.DeepEqual(apps, []string{"Test", "Test2"}) {
		t.Errorf("got %s, expected %s", apps, []string{"Test", "Test2"})
	}
}
