package state

import (
	"os"
	"testing"
	"time"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/config"
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

func TestPostgresState_RollbackFieldsRoundTrip(t *testing.T) {
	env := newPostgresTestEnv(t)

	t.Run("rollback fields persist and are read back", func(t *testing.T) {
		task := sampleTask("Rollback")
		task.IsRollback = true
		task.RollbackTargetId = "11111111-1111-4111-8111-111111111111"
		inserted := env.addTask(t, task)

		stored, err := env.state.GetTask(inserted.Id)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.True(t, stored.IsRollback)
		assert.Equal(t, "11111111-1111-4111-8111-111111111111", stored.RollbackTargetId)
	})

	t.Run("defaults apply when rollback fields are unset", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("NoRollback"))

		stored, err := env.state.GetTask(inserted.Id)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.False(t, stored.IsRollback)
		assert.Empty(t, stored.RollbackTargetId)
	})
}

func TestPostgresState_GetTasks(t *testing.T) {
	env := newPostgresTestEnv(t)

	start := float64(time.Now().Add(-time.Hour).Unix())
	env.addTask(t, sampleTask("Test"))
	env.addTask(t, sampleTask("Test2"))
	env.addTask(t, sampleTask("ObsoleteApp"))
	end := float64(time.Now().Add(time.Hour).Unix())

	tasks, total := env.state.GetTasks(start, end, "", "", 0, 0)
	assert.Len(t, tasks, 3)
	assert.Equal(t, int64(3), total)

	tasks, total = env.state.GetTasks(start, end, "Test", "", 0, 0)
	assert.Len(t, tasks, 1)
	assert.Equal(t, int64(1), total)

	tasks, total = env.state.GetTasks(start, end, "", models.StatusInProgressMessage, 0, 0)
	assert.Len(t, tasks, 3)
	assert.Equal(t, int64(3), total)

	tasks, total = env.state.GetTasks(start, end, "", "deployed", 0, 0)
	assert.Empty(t, tasks)
	assert.Equal(t, int64(0), total)
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

// TestPostgresState_GetTask_NotFound verifies that GetTask returns the
// ErrTaskNotFound sentinel (not a generic error) when no row matches, so the
// HTTP layer can map it to 404 while other failures surface as 500.
func TestPostgresState_GetTask_NotFound(t *testing.T) {
	env := newPostgresTestEnv(t)

	// Valid UUID that was never inserted -> gorm.ErrRecordNotFound.
	task, err := env.state.GetTask("00000000-0000-0000-0000-000000000000")
	assert.Nil(t, task)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

// TestPostgresState_GetTask_MalformedID verifies that a non-UUID id is mapped to
// ErrTaskNotFound (HTTP 404) rather than reaching the uuid-typed column and
// producing a client-triggerable backend error (HTTP 500). The parse guard runs
// before any query, so this does not need a live database.
func TestPostgresState_GetTask_MalformedID(t *testing.T) {
	state := &PostgresState{}
	task, err := state.GetTask("not-a-uuid")
	assert.Nil(t, task)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

// TestPostgresState_GetTask_BackendError verifies that a non-not-found backend
// failure (here: a closed connection pool) is returned as a wrapped error and
// NOT the ErrTaskNotFound sentinel, so a database outage keeps mapping to HTTP
// 500 instead of masquerading as a missing task.
func TestPostgresState_GetTask_BackendError(t *testing.T) {
	env := newPostgresTestEnv(t)

	db, err := env.state.orm.DB()
	require.NoError(t, err)
	require.NoError(t, db.Close())

	task, err := env.state.GetTask("00000000-0000-0000-0000-000000000000")
	assert.Nil(t, task)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTaskNotFound)
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

func TestPostgresState_CancelInProgressTasks(t *testing.T) {
	env := newPostgresTestEnv(t)

	inProgress := env.addTask(t, taskWithImage("app-a", "image-a"))
	sameAppOtherImage := env.addTask(t, taskWithImage("app-a", "image-b"))
	otherApp := env.addTask(t, taskWithImage("app-b", "image-a"))
	finished := env.addTask(t, taskWithImage("app-a", "image-a"))
	require.NoError(t, env.state.SetTaskStatus(finished.Id, models.StatusDeployedMessage, ""))

	count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the in-progress app-a task sharing image-a should be cancelled")

	got, err := env.state.GetTask(inProgress.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, got.Status)
	assert.Equal(t, "superseded", got.StatusReason)

	// Same app but a different image is untouched.
	gotSameApp, err := env.state.GetTask(sameAppOtherImage.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotSameApp.Status)

	gotOther, err := env.state.GetTask(otherApp.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotOther.Status)

	gotFinished, err := env.state.GetTask(finished.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusDeployedMessage, gotFinished.Status)
}

// TestPostgresState_CancelInProgressTasks_MultiImageOverlap mirrors the
// in-memory multi-image test: a task sharing one image name is cancelled while a
// fully disjoint task is left alone, exercising overlap (not equality) matching.
func TestPostgresState_CancelInProgressTasks_MultiImageOverlap(t *testing.T) {
	env := newPostgresTestEnv(t)

	overlapping := sampleTask("app-a")
	overlapping.Images = []models.Image{{Image: "image-a", Tag: "v1"}, {Image: "image-b", Tag: "v1"}}
	overlappingTask := env.addTask(t, overlapping)

	disjoint := sampleTask("app-a")
	disjoint.Images = []models.Image{{Image: "image-c", Tag: "v1"}, {Image: "image-d", Tag: "v1"}}
	disjointTask := env.addTask(t, disjoint)

	count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-b", Tag: "v2"}, {Image: "image-e", Tag: "v1"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the task sharing an image name should be cancelled")

	gotOverlapping, err := env.state.GetTask(overlappingTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotOverlapping.Status)

	gotDisjoint, err := env.state.GetTask(disjointTask.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, gotDisjoint.Status)
}

// TestPostgresState_CancelInProgressTasks_Count mirrors the in-memory count test
// for CI: no-overlap returns 0 (the len(ids) == 0 early return) and an
// overlapping deployment cancels every matching in-progress task.
func TestPostgresState_CancelInProgressTasks_Count(t *testing.T) {
	env := newPostgresTestEnv(t)

	first := env.addTask(t, taskWithImage("app-a", "image-a"))
	second := env.addTask(t, taskWithImage("app-a", "image-a"))

	count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-z", Tag: "v1"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "a deployment sharing no image should cancel nothing")

	count, err = env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "every matching in-progress task must be cancelled")

	gotFirst, err := env.state.GetTask(first.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotFirst.Status)
	gotSecond, err := env.state.GetTask(second.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotSecond.Status)
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
