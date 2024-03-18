package state

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	created       = float64(time.Now().Unix())
	postgresState = PostgresState{}

	deployedTaskId string
	deployedTask   = models.Task{
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
	}
	appNotFoundTaskId string
	appNotFoundTask   = models.Task{
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
	}
	abortedTaskId string
	abortedTask   = models.Task{
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
	}
)

func TestPostgresState_Add(t *testing.T) {
	databaseConfig := config.DatabaseConfig{
		Host:     os.Getenv("DB_HOST"),
		Port:     "5432",
		Name:     os.Getenv("DB_NAME"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASSWORD"),
	}
	testConfig := &config.ServerConfig{
		StateType: "postgres",
		Db:        databaseConfig,
	}
	err := postgresState.Connect(testConfig)
	assert.NoError(t, err)

	db, err := postgresState.orm.DB()
	assert.NoError(t, err)

	_, err = db.Exec("TRUNCATE TABLE tasks")
	assert.NoError(t, err)

	deployedTaskResult, err := postgresState.Add(deployedTask)
	assert.NoError(t, err)

	deployedTaskId = deployedTaskResult.Id

	appNotFoundTaskResult, err := postgresState.Add(appNotFoundTask)
	assert.NoError(t, err)

	appNotFoundTaskId = appNotFoundTaskResult.Id

	abortedTaskResult, err := postgresState.Add(abortedTask)
	assert.NoError(t, err)

	abortedTaskId = abortedTaskResult.Id
}

func TestPostgresState_GetTasks(t *testing.T) {
	// a temporary solution to wait for the task to be added to the database
	time.Sleep(1 * time.Second)

	// get all tasks without app filter
	tasks := postgresState.GetTasks(created, float64(time.Now().Unix()), "")
	assert.Len(t, tasks, 3)

	// get all tasks with app filter
	tasks = postgresState.GetTasks(created, float64(time.Now().Unix()), "Test")
	assert.Len(t, tasks, 1)
}

func TestPostgresState_GetTask(t *testing.T) {
	var task *models.Task
	var err error

	task, err = postgresState.GetTask(deployedTaskId)
	assert.NoError(t, err)

	assert.Equal(t, models.StatusInProgressMessage, task.Status)
}

func TestPostgresState_SetTaskStatus(t *testing.T) {
	var err error

	err = postgresState.SetTaskStatus(deployedTaskId, models.StatusDeployedMessage, "")
	assert.NoError(t, err)

	err = postgresState.SetTaskStatus(appNotFoundTaskId, models.StatusAppNotFoundMessage, "")
	assert.NoError(t, err)

	var taskInfo *models.Task
	taskInfo, _ = postgresState.GetTask(deployedTaskId)
	assert.Equal(t, models.StatusDeployedMessage, taskInfo.Status)

	taskInfo, _ = postgresState.GetTask(appNotFoundTaskId)
	assert.Equal(t, models.StatusAppNotFoundMessage, taskInfo.Status)
}

func TestPostgresState_GetAppList(t *testing.T) {
	apps := postgresState.GetAppList()

	for _, app := range postgresState.GetAppList() {
		assert.Equal(t, true, helpers.Contains([]string{"Test", "Test2", "ObsoleteApp"}, app))
	}

	assert.Len(t, apps, 3)
}

func TestPostgresState_ProcessObsoleteTasks(t *testing.T) {
	// set updated time to 2 hour ago for obsolete task
	updatedTime := time.Now().UTC().Add(-2 * time.Hour)

	// get connections
	db, err := postgresState.orm.DB()
	if err != nil {
		panic(err)
	}

	// update obsolete task
	if _, err := db.Exec("UPDATE tasks SET created = $1 WHERE id = $2", updatedTime, abortedTaskId); err != nil {
		t.Errorf("got error %s, expected nil", err.Error())
	}

	// update not found task
	if _, err := db.Exec("UPDATE tasks SET created = $1 WHERE id = $2", updatedTime, appNotFoundTaskId); err != nil {
		t.Errorf("got error %s, expected nil", err.Error())
	}

	postgresState.ProcessObsoleteTasks(1)

	// Check that obsolete task was deleted
	assert.Len(t, postgresState.GetAppList(), 2)

	// Check that task status was changed to aborted
	if task, err := postgresState.GetTask(abortedTaskId); err != nil {
		t.Errorf("got error %s, expected nil", err.Error())
	} else {
		assert.Equal(t, models.StatusAborted, task.Status)
	}
}

func TestPostgresState_Check(t *testing.T) {
	// Check that we return true if connection is ok
	assert.True(t, postgresState.Check())
}
