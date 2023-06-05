package state

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/shini4i/argo-watcher/internal/models"
)

const postgresTaskId = "782e6e84-e67d-11ec-9f2f-8a68373f0f50"

var (
	created       = float64(time.Now().Unix())
	updated       = created + 100
	postgresState = PostgresState{}
	postgresTasks = []models.Task{
		{
			Id:      postgresTaskId,
			Created: created,
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
			Created: created,
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

func TestPostgresState_Add(t *testing.T) {
	config := &config.ServerConfig{
		StateType:        "postgresql",
		DbHost:           os.Getenv("DB_HOST"),
		DbPort:           "5432",
		DbUser:           os.Getenv("DB_USER"),
		DbName:           os.Getenv("DB_NAME"),
		DbPassword:       os.Getenv("DB_PASSWORD"),
		DbMigrationsPath: "../../../db/migrations",
	}
	postgresState.Connect(config)
	_, err := postgresState.db.Exec("TRUNCATE TABLE tasks")
	if err != nil {
		panic(err)
	}
	for _, task := range postgresTasks {
		err := postgresState.Add(task)
		if err != nil {
			t.Errorf("got error %s, expected nil", err.Error())
		}
	}
}

func TestPostgresState_GetTask(t *testing.T) {
	var task *models.Task
	var err error

	if task, err = postgresState.GetTask(postgresTaskId); err != nil {
		t.Errorf("got error %s, expected nil", err.Error())
	}

	if task.Status != "in progress" {
		t.Errorf("got %s, expected %s", task.Status, "in progress")
	}
}

func TestPostgresState_SetTaskStatus(t *testing.T) {
	postgresState.SetTaskStatus(postgresTaskId, "deployed", "")

	if taskInfo, _ := postgresState.GetTask(postgresTaskId); taskInfo.Status != "deployed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "deployed")
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
