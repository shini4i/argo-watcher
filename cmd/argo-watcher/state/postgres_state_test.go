package state

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	deployedTaskId    = "782e6e84-e67d-11ec-9f2f-8a68373f0f50"
	appNotFoundTaskId = "5fa2d291-506a-42ab-804a-8bd75dba53e1"
	abortedTaskId     = "1c35d840-41d1-4b4f-a393-b8b71145686b"
)

var (
	created       = float64(time.Now().Unix())
	postgresState = PostgresState{}
	postgresTasks = []models.Task{
		{
			Id:      deployedTaskId,
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
			Id:      abortedTaskId,
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
		{
			Id:      appNotFoundTaskId,
			Created: created,
			App:     "ObsoleteApp",
			Author:  "Test Author",
			Project: "Test Project",
			Images: []models.Image{
				{
					Image: "test",
					Tag:   "v0.0.1",
				},
			},
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

	if task, err = postgresState.GetTask(deployedTaskId); err != nil {
		t.Errorf("got error %s, expected nil", err.Error())
	}

	if task.Status != "in progress" {
		t.Errorf("got %s, expected %s", task.Status, "in progress")
	}
}

func TestPostgresState_SetTaskStatus(t *testing.T) {
	postgresState.SetTaskStatus(deployedTaskId, "deployed", "")
	postgresState.SetTaskStatus(appNotFoundTaskId, "app not found", "")

	if taskInfo, _ := postgresState.GetTask(deployedTaskId); taskInfo.Status != "deployed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "deployed")
	}

	if taskInfo, _ := postgresState.GetTask(appNotFoundTaskId); taskInfo.Status != "app not found" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "app not found")
	}
}

func TestPostgresState_GetAppList(t *testing.T) {
	apps := postgresState.GetAppList()

	for _, app := range apps {
		if !helpers.Contains([]string{"Test", "Test2", "ObsoleteApp"}, app) {
			t.Errorf("Got unexpected value %s", app)
		}
	}

	if len(apps) != 3 {
		t.Errorf("Got %d apps, but expected %d", len(apps), 3)
	}
}

func TestPostgresState_processPostgresObsoleteTasks(t *testing.T) {
	// set updated time to 1 hour ago for obsolete task
	updatedTime := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := postgresState.db.Exec("UPDATE tasks SET created = $1 WHERE id = $2", updatedTime, abortedTaskId); err != nil {
		t.Errorf("got error %s, expected nil", err.Error())
	}

	err := processPostgresObsoleteTasks(postgresState.db)

	// Check that no error occurred
	assert.NoError(t, err)

	// Check that obsolete task was deleted
	assert.Len(t, postgresState.GetAppList(), 2)

	task, err := postgresState.GetTask(abortedTaskId)
	if err != nil {
		return
	}

	assert.Equal(t, "aborted", task.Status)
}
