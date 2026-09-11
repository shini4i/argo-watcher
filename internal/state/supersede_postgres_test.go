package state

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
)

// newImpatientState connects a state whose statements give up waiting on a row
// lock quickly, so a test can make one fail on demand instead of hanging. The
// lock_timeout rides on the DSN as a runtime parameter, the same trick
// newSchemalessState uses for search_path.
func newImpatientState(t *testing.T) *PostgresState {
	t.Helper()

	if os.Getenv("DB_DSN") == "" && os.Getenv("DB_HOST") == "" {
		t.Skip("Postgres integration tests require DB_DSN or DB_HOST to be configured")
	}

	databaseConfig, err := envConfig.ParseAs[config.DatabaseConfig]()
	require.NoError(t, err)
	databaseConfig.DSN += " lock_timeout=200"

	state := &PostgresState{}
	require.NoError(t, state.Connect(&config.ServerConfig{StateType: "postgres", Db: databaseConfig}))

	db, err := state.orm.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// A DSN that silently dropped the parameter would make the tests below hang
	// instead of exercising the failure they are about.
	var timeout string
	require.NoError(t, state.orm.Raw("SELECT current_setting('lock_timeout')").Scan(&timeout).Error)
	require.Equal(t, "200ms", timeout)

	return state
}

// holdRowLock takes an exclusive lock on one task row from its own connection and
// keeps it until the test ends, so any UPDATE of that row elsewhere times out.
func holdRowLock(t *testing.T, env *postgresTestEnv, id string) {
	t.Helper()

	db, err := env.state.orm.DB()
	require.NoError(t, err)

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	_, err = tx.Exec("SELECT id FROM tasks WHERE id = $1 FOR UPDATE", id)
	require.NoError(t, err)
}

// The savepoint is the only reason the supersede keeps its best-effort contract
// now that it shares a transaction with the insert: without it the failed UPDATE
// poisons the transaction and the deployment is refused, which is exactly the
// outcome best-effort exists to avoid.
func TestPostgresState_SupersedeFailureStillStoresTheTask(t *testing.T) {
	env := newPostgresTestEnv(t)

	blocked := env.addTask(t, sampleTask("app-savepoint"))
	holdRowLock(t, env, blocked.Id)

	newer := sampleTask("app-savepoint")
	added, cancelled, err := newImpatientState(t).SupersedeAndAdd(newer, "superseded")

	require.NoError(t, err, "a supersede that timed out must not refuse the deployment")
	require.NotNil(t, added)
	assert.Equal(t, models.StatusInProgressMessage, added.Status)
	assert.Equal(t, int64(0), cancelled, "nothing was superseded, and the count must say so")

	assertStatus(t, env.state, blocked.Id, models.StatusInProgressMessage)
}

// The failure direction of the atomicity: the old two-step code committed the
// cancellation and then failed the insert, leaving the app with a cancelled
// rollout and no replacement. One transaction is what makes that impossible.
func TestPostgresState_RejectedInsertRollsBackTheSupersede(t *testing.T) {
	env := newPostgresTestEnv(t)

	existing := env.addTask(t, sampleTask("app-rollback"))

	// app.author is varchar(255), so this insert is rejected after the supersede
	// has already updated the row.
	newer := sampleTask("app-rollback")
	newer.Author = strings.Repeat("a", 300)

	added, cancelled, err := env.state.SupersedeAndAdd(newer, "superseded")

	require.Error(t, err, "an insert the schema rejects must fail the whole submission")
	assert.Nil(t, added)
	assert.Equal(t, int64(0), cancelled)

	assertStatus(t, env.state, existing.Id, models.StatusInProgressMessage)
	got, err := env.state.GetTask(existing.Id)
	require.NoError(t, err)
	assert.Empty(t, got.StatusReason, "the rolled-back supersede must leave no reason behind")

	stored, _ := env.state.GetTasks(models.TaskFilter{EndTime: float64(time.Now().Add(time.Hour).Unix()), App: "app-rollback"})
	assert.Len(t, stored, 1, "the rejected task must not be stored")
}

// Advisory locks are one flat 64-bit namespace shared with the git write-back's
// per-repository keys, so the "app:" prefix is the only thing keeping an
// application from blocking on a repository's lock. Without it this hangs.
func TestPostgresState_AppLockDoesNotCollideWithRepositoryLocks(t *testing.T) {
	env := newPostgresTestEnv(t)

	db, err := env.state.orm.DB()
	require.NoError(t, err)

	// The key the write-back would take for a repository named "app-collision".
	held, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })
	_, err = held.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", lock.GenerateLockID("app-collision"))
	require.NoError(t, err)

	_, _, err = newImpatientState(t).SupersedeAndAdd(sampleTask("app-collision"), "superseded")
	require.NoError(t, err, "an application must not wait on the lock of a same-named repository")
}
