package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// insertTaskAt writes a row with explicit timestamps. AddTask lets the database
// choose `created`, which the window and median assertions below must control.
func (env *postgresTestEnv) insertTaskAt(t *testing.T, app, status, reason string, created, updated time.Time) {
	t.Helper()
	err := env.state.orm.Exec(
		`INSERT INTO tasks (created, updated, images, status, app, author, project, status_reason)
		 VALUES (?, ?, '[{"image":"acme/api","tag":"v1"}]'::jsonb, ?, ?, 'jane@example.com', 'acme/api', ?)`,
		created, updated, status, app, reason,
	).Error
	require.NoError(t, err)
}

func TestPostgresState_GetAppSummaries(t *testing.T) {
	env := newPostgresTestEnv(t)

	base := time.Now().UTC().Add(-2 * time.Hour)
	at := func(offset time.Duration) time.Time { return base.Add(offset) }

	env.insertTaskAt(t, "checkout", models.StatusDeployedMessage, "", at(0), at(10*time.Second))
	env.insertTaskAt(t, "checkout", models.StatusDeployedMessage, "", at(time.Minute), at(time.Minute+40*time.Second))
	env.insertTaskAt(t, "checkout", models.StatusFailedMessage, "Error: boom", at(2*time.Minute), at(2*time.Minute+30*time.Second))
	env.insertTaskAt(t, "checkout", models.StatusInProgressMessage, "", at(3*time.Minute), at(3*time.Minute))
	env.insertTaskAt(t, "payments", models.StatusAborted, "gave up", at(time.Minute), at(90*time.Second))
	// Outside the window: it must not reach any counter.
	env.insertTaskAt(t, "legacy", models.StatusDeployedMessage, "", base.Add(-time.Hour), base.Add(-time.Hour))

	filter := models.TaskFilter{
		StartTime: float64(base.Add(-time.Minute).Unix()),
		EndTime:   float64(time.Now().Add(time.Hour).Unix()),
	}
	summaries, err := env.state.GetAppSummaries(filter)
	require.NoError(t, err)
	require.Len(t, summaries, 2, "the out-of-window app must be absent")

	checkout := summaryByApp(summaries, "checkout")
	require.NotNil(t, checkout)
	assert.Equal(t, int64(4), checkout.Total)
	assert.Equal(t, int64(1), checkout.Failed)
	assert.Equal(t, int64(1), checkout.Running)
	assert.Equal(t, int64(2), checkout.Deployed)
	// Settled durations are 10s, 40s and 30s; the running task is excluded.
	assert.Equal(t, float64(30), checkout.MedianDurationSeconds)
	assert.Equal(t, models.StatusInProgressMessage, checkout.LastStatus)
	assert.Equal(t, "acme/api", checkout.Project)
	assert.Equal(t, []string{
		models.StatusInProgressMessage,
		models.StatusFailedMessage,
		models.StatusDeployedMessage,
		models.StatusDeployedMessage,
	}, checkout.RecentStatuses)

	// An aborted deployment counts as failed, matching IsFailedTaskStatus.
	payments := summaryByApp(summaries, "payments")
	require.NotNil(t, payments)
	assert.Equal(t, int64(1), payments.Failed)
	assert.Equal(t, "gave up", payments.LastStatusReason)
}

func TestPostgresState_GetAppSummaries_CapsRecentStatuses(t *testing.T) {
	env := newPostgresTestEnv(t)

	base := time.Now().UTC().Add(-time.Hour)
	total := models.RecentOutcomeLimit + 7
	for i := 0; i < total; i++ {
		offset := time.Duration(i) * time.Second
		env.insertTaskAt(t, "checkout", models.StatusDeployedMessage, "", base.Add(offset), base.Add(offset+time.Second))
	}

	summaries, err := env.state.GetAppSummaries(models.TaskFilter{
		StartTime: float64(base.Add(-time.Minute).Unix()),
		EndTime:   float64(time.Now().Add(time.Hour).Unix()),
	})
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	assert.Len(t, summaries[0].RecentStatuses, models.RecentOutcomeLimit)
	assert.Equal(t, int64(total), summaries[0].Total, "the counters see every row, not just the strip")
}

func TestPostgresState_GetAppSummaries_EmptyWindow(t *testing.T) {
	env := newPostgresTestEnv(t)

	summaries, err := env.state.GetAppSummaries(models.TaskFilter{
		StartTime: float64(time.Now().Add(-time.Minute).Unix()),
		EndTime:   float64(time.Now().Unix()),
	})
	require.NoError(t, err)
	assert.Empty(t, summaries)
}

func TestPostgresState_GetTasks_AuthorFilter(t *testing.T) {
	env := newPostgresTestEnv(t)

	jane := sampleTask("checkout")
	jane.Author = "Jane.Doe@example.com"
	env.addTask(t, jane)

	bot := sampleTask("checkout")
	bot.Author = "ci-bot@example.net"
	env.addTask(t, bot)

	window := models.TaskFilter{StartTime: 0, EndTime: float64(time.Now().Add(time.Hour).Unix())}

	t.Run("matches exactly, ignoring case", func(t *testing.T) {
		filter := window
		filter.Author = "jane.doe@example.com"
		tasks, total := env.state.GetTasks(filter)
		require.Equal(t, int64(1), total)
		assert.Equal(t, "Jane.Doe@example.com", tasks[0].Author)
	})

	t.Run("is not a substring match", func(t *testing.T) {
		filter := window
		filter.Author = "jane"
		_, total := env.state.GetTasks(filter)
		assert.Equal(t, int64(0), total)
	})

	// A LIKE metacharacter must not turn the exact match into a wildcard.
	t.Run("treats wildcards literally", func(t *testing.T) {
		filter := window
		filter.Author = "%"
		_, total := env.state.GetTasks(filter)
		assert.Equal(t, int64(0), total)
	})

	t.Run("composes with search rather than replacing it", func(t *testing.T) {
		filter := window
		filter.Author = "jane.doe@example.com"
		filter.Search = "nothing-here"
		_, total := env.state.GetTasks(filter)
		assert.Equal(t, int64(0), total)

		filter.Search = "checkout"
		_, total = env.state.GetTasks(filter)
		assert.Equal(t, int64(1), total)
	})
}

// The in-memory sort and the SQL must agree on which same-second task is newest;
// see TestInMemoryState_GetAppSummaries_BreaksASameSecondTieById.
func TestPostgresState_GetAppSummaries_BreaksASameSecondTieById(t *testing.T) {
	env := newPostgresTestEnv(t)

	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insert := func(id, status string) {
		t.Helper()
		err := env.state.orm.Exec(
			`INSERT INTO tasks (id, created, updated, images, status, app, author, project, status_reason)
			 VALUES (?, ?, ?, '[{"image":"acme/api","tag":"v1"}]'::jsonb, ?, 'checkout', 'jane@example.com', 'acme/api', '')`,
			id, at, at.Add(10*time.Second), status,
		).Error
		require.NoError(t, err)
	}

	insert("aaaaaaaa-0000-4000-8000-000000000001", models.StatusDeployedMessage)
	insert("bbbbbbbb-0000-4000-8000-000000000002", models.StatusFailedMessage)

	summaries, err := env.state.GetAppSummaries(models.TaskFilter{
		StartTime: float64(at.Add(-time.Minute).Unix()),
		EndTime:   float64(time.Now().Add(time.Hour).Unix()),
	})
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	assert.Equal(t, models.StatusFailedMessage, summaries[0].LastStatus)
	assert.Equal(t, []string{models.StatusFailedMessage, models.StatusDeployedMessage}, summaries[0].RecentStatuses)
}
