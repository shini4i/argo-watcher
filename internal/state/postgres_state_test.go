package state

import (
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state/state_models"
)

type postgresTestEnv struct {
	state *PostgresState
}

// Skips the test automatically when no Postgres configuration is present. Each
// option adjusts the server config the state connects with, so settings the
// state reads at connection time are exercised the way the server sets them.
func newPostgresTestEnv(t *testing.T, opts ...func(*config.ServerConfig)) *postgresTestEnv {
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
	for _, opt := range opts {
		opt(testConfig)
	}

	env := &postgresTestEnv{state: &PostgresState{}}
	require.NoError(t, env.state.Connect(testConfig))

	db, err := env.state.orm.DB()
	require.NoError(t, err)
	// Connect sets no connection limit, so a pool left open outlives its test and a
	// long run ends in "too many clients" rather than a real failure.
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("TRUNCATE TABLE tasks")
	require.NoError(t, err)

	return env
}

// newSchemalessState connects a second state whose search_path names a schema
// that does not exist, so every query fails on a missing relation. Nothing is
// altered, unlike a rename, which a killed test process would leave behind.
func newSchemalessState(t *testing.T) *PostgresState {
	t.Helper()

	if os.Getenv("DB_DSN") == "" && os.Getenv("DB_HOST") == "" {
		t.Skip("Postgres integration tests require DB_DSN or DB_HOST to be configured")
	}

	databaseConfig, err := envConfig.ParseAs[config.DatabaseConfig]()
	require.NoError(t, err)
	databaseConfig.DSN += " search_path=argo_watcher_no_such_schema"

	state := &PostgresState{}
	require.NoError(t, state.Connect(&config.ServerConfig{StateType: "postgres", Db: databaseConfig}))

	return state
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

	tasks, total, _ := env.state.GetTasks(models.TaskFilter{StartTime: start, EndTime: end})
	assert.Len(t, tasks, 3)
	assert.Equal(t, int64(3), total)

	tasks, total, _ = env.state.GetTasks(models.TaskFilter{StartTime: start, EndTime: end, App: "Test"})
	assert.Len(t, tasks, 1)
	assert.Equal(t, int64(1), total)

	tasks, total, _ = env.state.GetTasks(models.TaskFilter{StartTime: start, EndTime: end, Status: models.StatusInProgressMessage})
	assert.Len(t, tasks, 3)
	assert.Equal(t, int64(3), total)

	tasks, total, _ = env.state.GetTasks(models.TaskFilter{StartTime: start, EndTime: end, Status: "deployed"})
	assert.Empty(t, tasks)
	assert.Equal(t, int64(0), total)
}

func TestEscapeLikePattern(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "percent is escaped", value: "50%", want: `50\%`},
		{name: "underscore is escaped", value: "a_b", want: `a\_b`},
		{name: "backslash is escaped", value: `a\b`, want: `a\\b`},
		{name: "each metacharacter is escaped once", value: `a%b_c\d`, want: `a\%b\_c\\d`},
		{name: "plain text is unchanged", value: "checkout", want: "checkout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escapeLikePattern(tt.value))
		})
	}
}

func TestPostgresState_GetTasksSearch(t *testing.T) {
	env := newPostgresTestEnv(t)

	start := float64(time.Now().Add(-time.Hour).Unix())
	checkout := sampleTask("Checkout-API")
	checkout.Author = "Jane Doe"
	checkout.Images = []models.Image{{Image: "ghcr.io/acme/checkout", Tag: "v1.2.3"}}
	env.addTask(t, checkout)
	env.addTask(t, sampleTask("payments"))

	// Literal LIKE metacharacters in stored values: escaping must let an exact
	// term find these, not just stop a wildcard term from matching everything.
	literals := sampleTask("orders_api")
	literals.Author = "100% Bot"
	env.addTask(t, literals)
	end := float64(time.Now().Add(time.Hour).Unix())

	window := func(search string) models.TaskFilter {
		return models.TaskFilter{StartTime: start, EndTime: end, Search: search}
	}

	tests := []struct {
		name   string
		search string
		want   int64
	}{
		{name: "app substring, case-insensitively", search: "checkout-a", want: 1},
		{name: "author substring", search: "jane", want: 1},
		{name: "image name substring", search: "acme/check", want: 1},
		{name: "image and tag joined by a colon", search: "checkout:v1.2.3", want: 1},
		{name: "matches the task carrying the default author", search: "test author", want: 1},
		{name: "matches every task sharing an image tag", search: "v0.0.1", want: 2},
		{name: "no match", search: "nothing-here", want: 0},
		{name: "project is not searched", search: "test project", want: 0},
		// A wildcard term finds only the row holding that character, not all three.
		{name: "a percent sign matches literally", search: "%", want: 1},
		{name: "underscore is not a single-character wildcard", search: "_heckout", want: 0},
		{name: "a literal underscore is found", search: "orders_api", want: 1},
		{name: "a literal percent sign is found", search: "100%", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, total, _ := env.state.GetTasks(window(tt.search))
			assert.Equal(t, tt.want, total)
			assert.Len(t, tasks, int(tt.want))
		})
	}

	// The point of a server-side search: the page bounds the rows returned, not
	// the rows searched, and the total counts every match beyond the page.
	t.Run("pagination applies after the search", func(t *testing.T) {
		filter := window("v0.0.1")
		filter.Limit = 1
		tasks, total, _ := env.state.GetTasks(filter)
		assert.Len(t, tasks, 1)
		assert.Equal(t, int64(2), total)

		filter.Offset = 1
		second, total, _ := env.state.GetTasks(filter)
		assert.Len(t, second, 1)
		assert.Equal(t, int64(2), total)
		assert.NotEqual(t, tasks[0].Id, second[0].Id)
	})
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

// TestPostgresState_ValidatedFlagPersists locks the storage contract the
// authority rule depends on: whether a task presented a credential must survive
// the round trip, because SupersedeAndAdd reads it back from the row of a
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

// TestPostgresState_ProcessObsoleteTasksSparesLeasedTasks pins that the staleness
// sweep gives up only on tasks nobody is monitoring. A replica that resumes an
// abandoned deployment holds a live lease on a row that is already older than the
// window, and the monitor stops polling as soon as it reads "aborted" (issue #562).
func TestPostgresState_ProcessObsoleteTasksSparesLeasedTasks(t *testing.T) {
	env := newPostgresTestEnv(t)

	leased := env.addTask(t, sampleTask("LeasedApp"))
	lapsed := env.addTask(t, sampleTask("LapsedApp"))

	db, err := env.state.orm.DB()
	require.NoError(t, err)

	expired := time.Now().UTC().Add(-2 * time.Hour)
	_, err = db.Exec("UPDATE tasks SET created = $1", expired)
	require.NoError(t, err)

	require.NoError(t, env.state.ClaimTask(leased.Id))
	_, err = db.Exec("UPDATE tasks SET lease_expires_at = now() - interval '1 minute' WHERE id = $1", lapsed.Id)
	require.NoError(t, err)

	env.state.ProcessObsoleteTasks(1)

	monitored, err := env.state.GetTask(leased.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, monitored.Status)

	abandoned, err := env.state.GetTask(lapsed.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusAborted, abandoned.Status)
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

// withRetention configures the task retention window for a test environment.
func withRetention(days int) func(*config.ServerConfig) {
	return func(cfg *config.ServerConfig) {
		cfg.TaskRetentionEnabled = true
		cfg.TaskRetentionDays = days
	}
}

// addAgedTask stores a task and backdates it to the given age with the given
// status. Both columns are set by the database on insert, so a test that needs
// history has to rewrite them.
func (env *postgresTestEnv) addAgedTask(t *testing.T, app, status string, age time.Duration) *models.Task {
	t.Helper()
	task := env.addTask(t, sampleTask(app))
	require.NoError(t, env.state.orm.Exec(
		"UPDATE tasks SET status = ?, created = now() - make_interval(secs => ?) WHERE id = ?",
		status, age.Seconds(), task.Id).Error)
	return task
}

func (env *postgresTestEnv) taskExists(t *testing.T, id string) bool {
	t.Helper()
	var count int64
	require.NoError(t, env.state.orm.Model(&state_models.TaskModel{}).Where("id = ?", id).Count(&count).Error)
	return count > 0
}

func (env *postgresTestEnv) taskCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	require.NoError(t, env.state.orm.Model(&state_models.TaskModel{}).Count(&count).Error)
	return count
}

const day = 24 * time.Hour

// Deleting deployment history is irreversible, so nothing may be removed unless
// the operator turned retention on.
func TestPostgresState_TaskRetentionDisabled(t *testing.T) {
	env := newPostgresTestEnv(t)

	ancient := env.addAgedTask(t, "Ancient", models.StatusDeployedMessage, 3650*day)

	require.NoError(t, env.state.deleteExpiredTasks())

	assert.True(t, env.taskExists(t, ancient.Id),
		"retention is off, so no task may be deleted regardless of age")
}

func TestPostgresState_TaskRetention(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))

	expired := env.addAgedTask(t, "Expired", models.StatusDeployedMessage, 31*day)
	expiredFailed := env.addAgedTask(t, "ExpiredFailed", models.StatusFailedMessage, 400*day)
	recent := env.addAgedTask(t, "Recent", models.StatusDeployedMessage, 29*day)
	// An in-progress task may be claimed and actively monitored by a replica,
	// which would then write a status for a row that no longer exists.
	stuck := env.addAgedTask(t, "Stuck", models.StatusInProgressMessage, 400*day)

	require.NoError(t, env.state.deleteExpiredTasks())

	assert.False(t, env.taskExists(t, expired.Id))
	assert.False(t, env.taskExists(t, expiredFailed.Id))
	assert.True(t, env.taskExists(t, recent.Id), "a task inside the window must be kept")
	assert.True(t, env.taskExists(t, stuck.Id), "an in-progress task must never be deleted")
}

// The first sweep after enabling retention can face a table holding every
// deployment ever made, which is deleted in batches rather than one statement.
func TestPostgresState_TaskRetentionDrainsBacklogLargerThanOneBatch(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))

	backlog := retentionDeleteBatchSize + 500
	require.NoError(t, env.state.orm.Exec(`
		INSERT INTO tasks (created, updated, images, status, app, author, project)
		SELECT now() - make_interval(days => 400), now(), '[]'::jsonb, ?, 'Backlog', 'author', 'project'
		FROM generate_series(1, ?)`, models.StatusDeployedMessage, backlog).Error)
	require.Equal(t, int64(backlog), env.taskCount(t))

	require.NoError(t, env.state.deleteExpiredTasks())

	assert.Zero(t, env.taskCount(t), "every expired task must be removed, not just the first batch")
}

// Retention is driven by the hourly obsolete-task sweep, not by a loop of its own.
func TestPostgresState_TaskRetentionRunsWithinObsoleteSweep(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))

	expired := env.addAgedTask(t, "Expired", models.StatusDeployedMessage, 400*day)
	recent := env.addAgedTask(t, "Recent", models.StatusDeployedMessage, time.Hour)

	env.state.ProcessObsoleteTasks(1)

	assert.False(t, env.taskExists(t, expired.Id))
	assert.True(t, env.taskExists(t, recent.Id))
}

// The window is the one piece of arithmetic that decides what is destroyed, so
// it is pinned at the tightest legal setting rather than a day either side of a
// month, where an off-by-one would land within microseconds of the boundary.
func TestPostgresState_TaskRetentionWindowBoundary(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(1))

	past := env.addAgedTask(t, "JustPast", models.StatusDeployedMessage, 24*time.Hour+time.Minute)
	inside := env.addAgedTask(t, "JustInside", models.StatusDeployedMessage, 24*time.Hour-time.Minute)

	require.NoError(t, env.state.deleteExpiredTasks())

	assert.False(t, env.taskExists(t, past.Id), "a task one minute past the window is removed")
	assert.True(t, env.taskExists(t, inside.Id), "a task one minute inside the window is kept")
}

// A claim is what tells the sweep a task is somebody's work: while it is live the
// task is neither given up on nor collected, however old the row is. This pins both
// halves of the guard — the two paths differ only in whether the claim is live.
func TestPostgresState_TaskRetentionSparesTaskUnderActiveLease(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))

	// The HA case: a replica returning after an outage claims a rollout abandoned
	// long ago and resumes monitoring it. The row is past both the staleness and
	// the retention window, so a sweep blind to the claim would abort the
	// deployment and then delete the row the replica is about to write to.
	resumed := env.addAgedTask(t, "Resumed", models.StatusInProgressMessage, 400*day)
	require.NoError(t, env.state.ClaimTask(resumed.Id))

	env.state.ProcessObsoleteTasks(1)

	require.True(t, env.taskExists(t, resumed.Id),
		"a task under an unexpired lease must survive, however old it is")

	stored, err := env.state.GetTask(resumed.Id)
	require.NoError(t, err)
	assert.Equal(t, models.StatusInProgressMessage, stored.Status,
		"the rollout is still being monitored, so the sweep must not give up on it")

	held, err := env.state.RenewLease(resumed.Id)
	require.NoError(t, err)
	assert.True(t, held, "the replica monitoring it must still hold its claim")

	// Once the claim lapses the row is nobody's: the same pass gives up on it and
	// then collects it.
	env.expireLease(t, resumed.Id)
	env.state.ProcessObsoleteTasks(1)

	assert.False(t, env.taskExists(t, resumed.Id),
		"a lapsed lease no longer protects a task past the window")
}

// Every replica runs the sweep, so sweeps contend for the same rows — something
// sequential tests never show. What is pinned here is the outcome: no sweep
// fails, deadlocks, or double-counts, and between them they leave nothing
// behind. Whether SKIP LOCKED made them step over each other or merely queue is
// deliberately not asserted; both drain the backlog, and only the first is
// observable under load a test cannot reproduce.
func TestPostgresState_TaskRetentionConcurrentSweeps(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))

	const expired = 2500
	require.NoError(t, env.state.orm.Exec(`
		INSERT INTO tasks (created, updated, images, status, app, author, project)
		SELECT now() - make_interval(days => 400), now(), '[]'::jsonb, ?, 'Raced', 'author', 'project'
		FROM generate_series(1, ?)`, models.StatusDeployedMessage, expired).Error)
	require.Equal(t, int64(expired), env.taskCount(t))

	const sweepers = 4
	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		mu    sync.Mutex
		errs  []error
	)

	for range sweepers {
		replica := env.secondReplica(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := replica.deleteExpiredTasks()

			mu.Lock()
			defer mu.Unlock()
			errs = append(errs, err)
		}()
	}

	close(start)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err, "a sweep must not fail because another was running")
	}
	// A row one sweeper skipped was locked by another that deletes it before
	// returning, so nothing survives all four.
	assert.Zero(t, env.taskCount(t), "concurrent sweeps must between them remove every expired task")
}

// The sweep funnels three steps into one "Couldn't process obsolete tasks" log
// line, so the prefix is how an operator tells a failed retention pass from the
// app-not-found delete or the stale-abort update. Only the prefix is asserted:
// the driver's own wording is internal to database/sql and has no stable
// sentinel to match on.
func TestPostgresState_TaskRetentionSweepErrorNamesTheStep(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))
	env.addAgedTask(t, "Expired", models.StatusDeployedMessage, 31*day)

	sqlDB, err := env.state.orm.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = env.state.deleteExpiredTasks()

	require.Error(t, err)
	assert.ErrorContains(t, err, "deleting tasks past the 30 day retention window")
	assert.NotNil(t, errors.Unwrap(err), "the driver cause must stay wrapped with %w")
}

// The other half of the lease guard: an abandoned rollout nobody claimed is
// aborted for staleness and collected by that same pass, which is what the
// retention step running last buys. Only a live claim defers it.
func TestPostgresState_TaskRetentionCollectsUnleasedTaskAbortedByTheSamePass(t *testing.T) {
	env := newPostgresTestEnv(t, withRetention(30))

	unclaimed := env.addAgedTask(t, "Unclaimed", models.StatusInProgressMessage, 400*day)

	env.state.ProcessObsoleteTasks(1)

	assert.False(t, env.taskExists(t, unclaimed.Id),
		"with no replica holding it, a stale task is aborted and removed by the one sweep")
}

func TestPostgresState_Contract(t *testing.T) {
	// Guard at the top level so an unconfigured database reports SKIP for this test
	// rather than PASS with every child skipped.
	newPostgresTestEnv(t)

	runTaskRepositoryContract(t, func(t *testing.T) TaskRepository {
		return newPostgresTestEnv(t).state
	})
}

// Re-swallowing the read at the backend would put the finding straight back: an
// outage would render as an empty page again, and every caller above believes the
// interface. This reaches the Count branch, which runs first.
func TestPostgresState_GetTasks_ReportsABackendFailure(t *testing.T) {
	state := newSchemalessState(t)

	tasks, total, err := state.GetTasks(models.TaskFilter{
		EndTime: float64(time.Now().Add(time.Hour).Unix()),
	})

	require.Error(t, err, "a failed read must never be reported as an empty page")
	assert.Empty(t, tasks)
	assert.Zero(t, total)
}

// created is a timestamptz, so a tie here needs microsecond-identical rows and id
// breaks it only to keep a page totally ordered across offsets. detectRollback reads
// the first deployed task as the current version, so an unstable first row makes
// rollback detection non-deterministic on the backend that runs in production.
func TestPostgresState_GetTasksBreaksAnExactTieById(t *testing.T) {
	env := newPostgresTestEnv(t)

	var ids []string
	for range 3 {
		ids = append(ids, env.addTask(t, sampleTask("app-tie")).Id)
	}

	// Forced, because Postgres stores microseconds and would not otherwise tie.
	db, err := env.state.orm.DB()
	require.NoError(t, err)
	_, err = db.Exec("UPDATE tasks SET created = now() WHERE app = $1", "app-tie")
	require.NoError(t, err)

	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	for range 5 {
		tasks, _, err := env.state.GetTasks(models.TaskFilter{
			EndTime: float64(time.Now().Add(time.Hour).Unix()),
			App:     "app-tie",
		})
		require.NoError(t, err)
		require.Len(t, tasks, 3)
		assert.Equal(t, ids, []string{tasks[0].Id, tasks[1].Id, tasks[2].Id},
			"an exact tie must resolve by id so paging stays total")
	}
}

// The count and the row fetch fail independently, and only the count is covered by
// the schemaless state — it never reaches the fetch. A fetch whose error was
// dropped would serve an empty page beside a non-zero total and no error at all,
// which is the shape this change set exists to make impossible.
func TestPostgresState_GetTasks_ReportsAFailedRowFetch(t *testing.T) {
	env := newPostgresTestEnv(t)
	env.addTask(t, sampleTask("app-fetch"))

	// The count runs first and must succeed, so the failure is injected on the
	// second query of the call. Registered on this env's handle only.
	var queries int
	require.NoError(t, env.state.orm.Callback().Query().Before("gorm:query").
		Register("test:fail_second_query", func(db *gorm.DB) {
			queries++
			if queries == 2 {
				_ = db.AddError(errors.New("row fetch failed"))
			}
		}))

	tasks, total, err := env.state.GetTasks(models.TaskFilter{
		EndTime: float64(time.Now().Add(time.Hour).Unix()),
		App:     "app-fetch",
	})

	require.Error(t, err, "a failed row fetch must not be served as an empty page")
	assert.Empty(t, tasks)
	assert.Zero(t, total, "a total beside an error would read as a real count")
}
