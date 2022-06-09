package state

import (
	"github.com/google/uuid"
	"os"
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/cmd/helpers"
	m "github.com/shini4i/argo-watcher/cmd/models"
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

func TestPostgresState_SetTaskStatus(t *testing.T) {
	postgresState.SetTaskStatus(postgresTaskId, "deployed")

	if status := postgresState.GetTaskStatus(postgresTaskId); status != "deployed" {
		t.Errorf("got %s, expected %s", status, "deployed")
	}
}

func TestPostgresState_GetAppList(t *testing.T) {
	apps := postgresState.GetAppList()

	for _, app := range apps {
		if !helpers.Contains([]string{"Test", "Test2"}, app) {
			t.Errorf("Got unexpected value %s", app)
		}
	}

	if len(apps) != 2 {
		t.Errorf("Got %d apps, but expected %d", len(apps), 2)
	}
}
