package state

import (
	"os"
	"testing"
	"time"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type postgresTestEnv struct {
	state *PostgresState
}

// newPostgresTestEnv prepares an isolated Postgres-backed repository for integration testing.
// Tests are skipped automatically when no Postgres configuration is present in the environment.
func newPostgresTestEnv(t *testing.T) *postgresTestEnv {
	t.Helper()

	if os.Getenv("DB_DSN") == "" && os.Getenv("DB_HOST") == "" {
		t.Skip("Postgres integration tests require DB_DSN or DB_HOST to be configured")
	}

	databaseConfig, err := envConfig.ParseAs[config.DatabaseConfig]()
	require.NoError(t, err)

	testConfig := &config.ServerConfig{
		StateType: "postgres",
		Db:        databaseConfig,
	}

	env := &postgresTestEnv{state: &PostgresState{}}
	require.NoError(t, env.state.Connect(testConfig))

	db, err := env.state.orm.DB()
	require.NoError(t, err)

	_, err = db.Exec("TRUNCATE TABLE tasks")
	require.NoError(t, err)

	return env
}

// addTask persists a task fixture and returns the stored record.
func (env *postgresTestEnv) addTask(t *testing.T, task models.Task) *models.Task {
	t.Helper()
	result, err := env.state.AddTask(task)
	require.NoError(t, err)
	return result
}

// sampleTask builds a reusable task definition for integration tests.
func sampleTask(app string) models.Task {
	return models.Task{
		App:     app,
		Author:  "Test Author",
		Project: "Test Project",
		Images: []models.Image{
			{Image: "test", Tag: "v0.0.1"},
		},
	}
}

func TestPostgresState_AddTask(t *testing.T) {
	env := newPostgresTestEnv(t)

	task := sampleTask("Test")
	result := env.addTask(t, task)

	assert.NotEmpty(t, result.Id)
	assert.Equal(t, models.StatusInProgressMessage, result.Status)
	assert.Equal(t, "Test", result.App)
}

func TestPostgresState_GetTasks(t *testing.T) {
	env := newPostgresTestEnv(t)

	start := float64(time.Now().Add(-time.Hour).Unix())
	env.addTask(t, sampleTask("Test"))
	env.addTask(t, sampleTask("Test2"))
	env.addTask(t, sampleTask("ObsoleteApp"))
	end := float64(time.Now().Add(time.Hour).Unix())

	tasks, total := env.state.GetTasks(start, end, "", 0, 0)
	assert.Len(t, tasks, 3)
	assert.Equal(t, int64(3), total)

	tasks, total = env.state.GetTasks(start, end, "Test", 0, 0)
	assert.Len(t, tasks, 1)
	assert.Equal(t, int64(1), total)
}

func TestPostgresState_GetTask(t *testing.T) {
	env := newPostgresTestEnv(t)
	inserted := env.addTask(t, sampleTask("Test"))

	task, err := env.state.GetTask(inserted.Id)
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, inserted.Id, task.Id)
	assert.Equal(t, models.StatusInProgressMessage, task.Status)
}

func TestPostgresState_SetTaskStatus(t *testing.T) {
	env := newPostgresTestEnv(t)
	inserted := env.addTask(t, sampleTask("Test"))

	err := env.state.SetTaskStatus(inserted.Id, models.StatusDeployedMessage, "finished")
	assert.NoError(t, err)

	taskInfo, err := env.state.GetTask(inserted.Id)
	require.NoError(t, err)
	require.NotNil(t, taskInfo)
	assert.Equal(t, models.StatusDeployedMessage, taskInfo.Status)
	assert.Equal(t, "finished", taskInfo.StatusReason)
}

func TestPostgresState_ProcessObsoleteTasks(t *testing.T) {
	env := newPostgresTestEnv(t)

	obsolete := env.addTask(t, sampleTask("ObsoleteApp"))
	appNotFound := env.addTask(t, sampleTask("Test2"))

	db, err := env.state.orm.DB()
	require.NoError(t, err)

	expired := time.Now().UTC().Add(-2 * time.Hour)
	_, err = db.Exec("UPDATE tasks SET created = $1 WHERE id = $2", expired, obsolete.Id)
	require.NoError(t, err)

	_, err = db.Exec("UPDATE tasks SET status = $1, created = $2 WHERE id = $3", models.StatusAppNotFoundMessage, expired, appNotFound.Id)
	require.NoError(t, err)

	env.state.ProcessObsoleteTasks(1)

	_, err = env.state.GetTask(appNotFound.Id)
	assert.Error(t, err)

	task, err := env.state.GetTask(obsolete.Id)
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, models.StatusAborted, task.Status)
}

func TestPostgresState_Check(t *testing.T) {
	env := newPostgresTestEnv(t)
	assert.True(t, env.state.Check())
}
