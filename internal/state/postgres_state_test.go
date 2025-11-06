package state

import (
	"testing"
	"time"

	envConfig "github.com/caarlos0/env/v11"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
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

func TestPostgresState_AddTask(t *testing.T) {
	var err error
	var databaseConfig config.DatabaseConfig

	databaseConfig, err = envConfig.ParseAs[config.DatabaseConfig]()
	assert.NoError(t, err)

	testConfig := &config.ServerConfig{
		StateType: "postgres",
		Db:        databaseConfig,
	}
	err = postgresState.Connect(testConfig)
	assert.NoError(t, err)

	db, err := postgresState.orm.DB()
	assert.NoError(t, err)

	_, err = db.Exec("TRUNCATE TABLE tasks")
	assert.NoError(t, err)

	deployedTaskResult, err := postgresState.AddTask(deployedTask)
	assert.NoError(t, err)

	deployedTaskId = deployedTaskResult.Id

	appNotFoundTaskResult, err := postgresState.AddTask(appNotFoundTask)
	assert.NoError(t, err)

	appNotFoundTaskId = appNotFoundTaskResult.Id

	abortedTaskResult, err := postgresState.AddTask(abortedTask)
	assert.NoError(t, err)

	abortedTaskId = abortedTaskResult.Id
}

func TestPostgresState_GetTasks(t *testing.T) {
	// a temporary solution to wait for the task to be added to the database
	time.Sleep(1 * time.Second)

	// get all tasks without app filter
	tasks, total := postgresState.GetTasks(created, float64(time.Now().Unix()), "", 0, 0)
	assert.Len(t, tasks, 3)
	assert.Equal(t, int64(3), total)

	// get all tasks with app filter
	tasks, total = postgresState.GetTasks(created, float64(time.Now().Unix()), "Test", 0, 0)
	assert.Len(t, tasks, 1)
	assert.Equal(t, int64(1), total)
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

func TestPostgresState_ProcessObsoleteTasks(t *testing.T) {
	// set updated time to 2 hour ago for obsolete task
	updatedTime := time.Now().UTC().Add(-2 * time.Hour)

	// get connections
	db, err := postgresState.orm.DB()
	assert.NoError(t, err)

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
	startTime := float64(time.Now().Unix()) - 10
	endTime := float64(time.Now().Unix())
	tasks, _ := postgresState.GetTasks(startTime, endTime, "", 0, 0)
	for _, task := range tasks {
		assert.NotEqual(t, appNotFoundTask, task.Id)
	}

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
