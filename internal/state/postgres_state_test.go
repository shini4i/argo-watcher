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
	"github.com/shini4i/argo-watcher/internal/state/state_models"
)

type postgresTestEnv struct {
	state *PostgresState
}

// Skips the test automatically when no Postgres configuration is present.
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

func (env *postgresTestEnv) addTask(t *testing.T, task models.Task) *models.Task {
	t.Helper()
	result, err := env.state.AddTask(task)
	require.NoError(t, err)
	return result
}

// storedModel reads the persisted row directly so tests can assert on columns
// that are deliberately not surfaced on the external task (notably validated,
// which is kept out of API responses).
func (env *postgresTestEnv) storedModel(t *testing.T, id string) state_models.TaskModel {
	t.Helper()
	var stored state_models.TaskModel
	require.NoError(t, env.state.orm.Take(&stored, "id = ?", id).Error)
	return stored
}

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

	count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the in-progress app-a task sharing image-a should be cancelled")

	got, err := env.state.GetTask(inProgress.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, got.Status)
	assert.Equal(t, "superseded", got.StatusReason)

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

	count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-b", Tag: "v2"}, {Image: "image-e", Tag: "v1"}}, "superseded", false)
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

	count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-z", Tag: "v1"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "a deployment sharing no image should cancel nothing")

	count, err = env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "every matching in-progress task must be cancelled")

	gotFirst, err := env.state.GetTask(first.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotFirst.Status)
	gotSecond, err := env.state.GetTask(second.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelledMessage, gotSecond.Status)
}

// TestPostgresState_ValidatedFlagPersists locks the storage contract the
// authority rule depends on: whether a task presented a credential must survive
// the round trip, because CancelInProgressTasks reads it back from the row of a
// task that may have been created by another replica.
func TestPostgresState_ValidatedFlagPersists(t *testing.T) {
	env := newPostgresTestEnv(t)

	t.Run("validated is stored and read back", func(t *testing.T) {
		task := sampleTask("Credentialed")
		task.Validated = true
		inserted := env.addTask(t, task)

		assert.True(t, env.storedModel(t, inserted.Id).Validated)
	})

	t.Run("defaults to false when no credential was presented", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Anonymous"))

		assert.False(t, env.storedModel(t, inserted.Id).Validated)
	})

	t.Run("is not mapped back onto the external task", func(t *testing.T) {
		task := sampleTask("Credentialed")
		task.Validated = true
		inserted := env.addTask(t, task)

		stored, err := env.state.GetTask(inserted.Id)
		require.NoError(t, err)
		assert.False(t, stored.Validated,
			"a re-read task must not claim authority; write-back reads the in-process task, not this one")
	})
}

// TestPostgresState_CancelInProgressTasks_Authority mirrors the in-memory
// authority test against real Postgres: an uncredentialed deployment must not
// cancel a credentialed in-flight rollout, while every other combination still
// supersedes. Running it here matters because the Postgres path filters
// candidates in Go after reading them back, so the column must be selected.
func TestPostgresState_CancelInProgressTasks_Authority(t *testing.T) {
	tests := []struct {
		name             string
		victimValidated  bool
		newTaskValidated bool
		wantCancelled    bool
	}{
		{"unvalidated must not cancel validated", true, false, false},
		{"validated cancels validated", true, true, true},
		{"unvalidated cancels unvalidated", false, false, true},
		{"validated cancels unvalidated", false, true, true},
	}

	// Guard at the top level so an unconfigured database reports SKIP for this test
	// rather than PASS with four skipped children — this is the only unit-level
	// cover for the Postgres half of the rule, and a green no-op would hide that.
	// Each case still builds its own env, which truncates tasks for isolation.
	newPostgresTestEnv(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newPostgresTestEnv(t)

			victim := taskWithImage("app-a", "image-a")
			victim.Validated = tt.victimValidated
			inFlight := env.addTask(t, victim)

			count, err := env.state.CancelInProgressTasks("app-a", []models.Image{{Image: "image-a", Tag: "v2"}}, "superseded", tt.newTaskValidated)
			require.NoError(t, err)

			got, err := env.state.GetTask(inFlight.Id)
			require.NoError(t, err)

			if tt.wantCancelled {
				assert.Equal(t, int64(1), count)
				assert.Equal(t, models.StatusCancelledMessage, got.Status)
				return
			}
			assert.Equal(t, int64(0), count)
			assert.Equal(t, models.StatusInProgressMessage, got.Status)
		})
	}
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
	assert.Equal(t, StaleTaskAbortReason, task.StatusReason)
}

func TestPostgresState_Check(t *testing.T) {
	env := newPostgresTestEnv(t)
	assert.True(t, env.state.Check())
}

// TestPostgresState_ResumptionFieldsPersist locks the storage contract the HA
// handoff depends on: a replica that claims a task after its owner stopped
// rebuilds it from the row alone, so the row must carry every setting the
// rollout acts on. A dropped timeout silently re-deadlines the deployment; a
// dropped refresh override silently changes how its status is read.
func TestPostgresState_ResumptionFieldsPersist(t *testing.T) {
	env := newPostgresTestEnv(t)

	t.Run("timeout and refresh overrides survive the round trip", func(t *testing.T) {
		refresh := true
		task := sampleTask("Overridden")
		task.Timeout = 900
		task.Refresh = &refresh
		task.Validated = true
		inserted := env.addTask(t, task)

		stored := env.storedModel(t, inserted.Id)
		assert.Equal(t, 900, stored.Timeout)
		require.True(t, stored.Refresh.Valid)
		assert.True(t, stored.Refresh.Bool)

		resumed := stored.ConvertToResumedTask()
		assert.Equal(t, 900, resumed.Timeout)
		require.NotNil(t, resumed.Refresh)
		assert.True(t, *resumed.Refresh)
		assert.True(t, resumed.Validated)
	})

	t.Run("an omitted refresh stays null rather than becoming false", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Default"))

		stored := env.storedModel(t, inserted.Id)
		assert.Equal(t, 0, stored.Timeout)
		assert.False(t, stored.Refresh.Valid,
			"NULL means the instance default applies; false would force refresh off")
		assert.Nil(t, stored.ConvertToResumedTask().Refresh)
	})

	t.Run("an explicit false is stored as false, not null", func(t *testing.T) {
		refresh := false
		task := sampleTask("ExplicitOff")
		task.Refresh = &refresh
		inserted := env.addTask(t, task)

		stored := env.storedModel(t, inserted.Id)
		require.True(t, stored.Refresh.Valid)
		assert.False(t, stored.Refresh.Bool)
	})

	t.Run("a fresh task is unowned until a replica claims it", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Unowned"))

		stored := env.storedModel(t, inserted.Id)
		assert.False(t, stored.OwnerId.Valid)
		assert.False(t, stored.LeaseExpiresAt.Valid)
	})
}
