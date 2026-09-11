package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// summaryTask builds a settled task with explicit timestamps, so a test can
// place it in or out of the window and control its duration exactly.
func summaryTask(app, status string, created, updated float64) models.Task {
	task := createTestTask(app)
	task.Status = status
	task.Created = created
	task.Updated = updated
	return task
}

func seedSummaryState(tasks ...models.Task) *InMemoryState {
	return &InMemoryState{tasks: tasks}
}

func summaryByApp(summaries []models.AppSummary, app string) *models.AppSummary {
	for i := range summaries {
		if summaries[i].App == app {
			return &summaries[i]
		}
	}
	return nil
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		want   float64
	}{
		{name: "empty", sorted: nil, want: 0},
		{name: "single", sorted: []float64{7}, want: 7},
		{name: "odd count takes the middle", sorted: []float64{1, 5, 9}, want: 5},
		{name: "even count averages the two middles", sorted: []float64{2, 4, 6, 8}, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, median(tt.sorted))
		})
	}
}

func TestInMemoryState_GetAppSummaries_Counts(t *testing.T) {
	state := seedSummaryState(
		summaryTask("checkout", models.StatusDeployedMessage, 100, 160),
		summaryTask("checkout", models.StatusFailedMessage, 200, 220),
		summaryTask("checkout", models.StatusInProgressMessage, 300, 300),
		summaryTask("checkout", models.StatusCancelledMessage, 400, 410),
		summaryTask("payments", models.StatusDeployedMessage, 150, 200),
	)

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 0, EndTime: 1000})
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	checkout := summaryByApp(summaries, "checkout")
	require.NotNil(t, checkout)
	assert.Equal(t, int64(4), checkout.Total)
	assert.Equal(t, int64(1), checkout.Failed)
	assert.Equal(t, int64(1), checkout.Running)
	assert.Equal(t, int64(1), checkout.Deployed)
	// The cancelled task lands in none of the three counters but still in Total.
	assert.Equal(t, int64(3), checkout.Failed+checkout.Running+checkout.Deployed)
}

func TestInMemoryState_GetAppSummaries_GroupsAbortedWithFailed(t *testing.T) {
	state := seedSummaryState(
		summaryTask("checkout", models.StatusAborted, 100, 160),
		summaryTask("checkout", models.StatusArgoCDUnavailableMessage, 200, 220),
		summaryTask("checkout", models.StatusAppNotFoundMessage, 300, 310),
	)

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 0, EndTime: 1000})
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	// "app not found" is a misconfiguration, not a failed rollout.
	assert.Equal(t, int64(2), summaries[0].Failed)
}

func TestInMemoryState_GetAppSummaries_HonoursOnlyTheWindow(t *testing.T) {
	state := seedSummaryState(
		summaryTask("checkout", models.StatusDeployedMessage, 50, 60),
		summaryTask("checkout", models.StatusDeployedMessage, 500, 510),
		summaryTask("payments", models.StatusFailedMessage, 500, 510),
	)

	// App and status are set but must be ignored: the summary is a census.
	filter := models.TaskFilter{
		StartTime: 100,
		EndTime:   1000,
		App:       "checkout",
		Status:    models.StatusFailedMessage,
	}
	summaries, err := state.GetAppSummaries(filter)
	require.NoError(t, err)

	require.Len(t, summaries, 2)
	assert.Equal(t, int64(1), summaryByApp(summaries, "checkout").Total)
	assert.Equal(t, int64(1), summaryByApp(summaries, "payments").Total)
}

func TestInMemoryState_GetAppSummaries_MedianSkipsRunningTasks(t *testing.T) {
	state := seedSummaryState(
		summaryTask("checkout", models.StatusDeployedMessage, 100, 110),
		summaryTask("checkout", models.StatusDeployedMessage, 200, 240),
		summaryTask("checkout", models.StatusDeployedMessage, 300, 330),
		// A running task has no duration yet; counting 0 would drag the median down.
		summaryTask("checkout", models.StatusInProgressMessage, 400, 400),
	)

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 0, EndTime: 1000})
	require.NoError(t, err)
	assert.Equal(t, float64(30), summaries[0].MedianDurationSeconds)
}

func TestInMemoryState_GetAppSummaries_MedianIsZeroWithoutSettledTasks(t *testing.T) {
	state := seedSummaryState(summaryTask("checkout", models.StatusInProgressMessage, 100, 100))

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 0, EndTime: 1000})
	require.NoError(t, err)
	assert.Equal(t, float64(0), summaries[0].MedianDurationSeconds)
}

func TestInMemoryState_GetAppSummaries_DescribesTheNewestTask(t *testing.T) {
	newest := summaryTask("checkout", models.StatusFailedMessage, 900, 950)
	newest.StatusReason = "Error: boom"
	newest.Project = "https://git.example.net/acme/checkout"

	state := seedSummaryState(
		summaryTask("checkout", models.StatusDeployedMessage, 100, 110),
		newest,
		summaryTask("checkout", models.StatusDeployedMessage, 500, 510),
	)

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 0, EndTime: 1000})
	require.NoError(t, err)

	assert.Equal(t, models.StatusFailedMessage, summaries[0].LastStatus)
	assert.Equal(t, "Error: boom", summaries[0].LastStatusReason)
	assert.Equal(t, float64(900), summaries[0].LastCreated)
	assert.Equal(t, "https://git.example.net/acme/checkout", summaries[0].Project)
}

func TestInMemoryState_GetAppSummaries_RecentStatusesNewestFirstAndCapped(t *testing.T) {
	tasks := make([]models.Task, 0, models.RecentOutcomeLimit+5)
	for i := 0; i < models.RecentOutcomeLimit+5; i++ {
		status := models.StatusDeployedMessage
		if i == models.RecentOutcomeLimit+4 {
			status = models.StatusFailedMessage
		}
		tasks = append(tasks, summaryTask("checkout", status, float64(100+i), float64(105+i)))
	}

	summaries, err := seedSummaryState(tasks...).GetAppSummaries(
		models.TaskFilter{StartTime: 0, EndTime: 1000},
	)
	require.NoError(t, err)

	require.Len(t, summaries[0].RecentStatuses, models.RecentOutcomeLimit)
	assert.Equal(t, models.StatusFailedMessage, summaries[0].RecentStatuses[0])
	assert.Equal(t, int64(models.RecentOutcomeLimit+5), summaries[0].Total)
}

func TestInMemoryState_GetAppSummaries_SortedByApp(t *testing.T) {
	state := seedSummaryState(
		summaryTask("zebra", models.StatusDeployedMessage, 100, 110),
		summaryTask("alpha", models.StatusDeployedMessage, 100, 110),
		summaryTask("middle", models.StatusDeployedMessage, 100, 110),
	)

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 0, EndTime: 1000})
	require.NoError(t, err)

	assert.Equal(t, []string{"alpha", "middle", "zebra"}, []string{
		summaries[0].App, summaries[1].App, summaries[2].App,
	})
}

func TestInMemoryState_GetAppSummaries_EmptyWindow(t *testing.T) {
	state := seedSummaryState(summaryTask("checkout", models.StatusDeployedMessage, 50, 60))

	summaries, err := state.GetAppSummaries(models.TaskFilter{StartTime: 100, EndTime: 1000})
	require.NoError(t, err)
	assert.Empty(t, summaries)
}

func TestInMemoryState_GetTasks_AuthorFilter(t *testing.T) {
	jane := summaryTask("checkout", models.StatusDeployedMessage, 100, 110)
	jane.Author = "Jane.Doe@example.com"
	bot := summaryTask("checkout", models.StatusDeployedMessage, 200, 210)
	bot.Author = "ci-bot@example.net"

	state := seedSummaryState(jane, bot)
	window := models.TaskFilter{StartTime: 0, EndTime: 1000}

	t.Run("matches exactly, ignoring case", func(t *testing.T) {
		filter := window
		filter.Author = "jane.doe@example.com"
		tasks, total, _ := state.GetTasks(filter)
		require.Equal(t, int64(1), total)
		assert.Equal(t, "Jane.Doe@example.com", tasks[0].Author)
	})

	t.Run("is not a substring match", func(t *testing.T) {
		filter := window
		filter.Author = "jane"
		_, total, _ := state.GetTasks(filter)
		assert.Equal(t, int64(0), total)
	})

	// Author scopes to a person; search still narrows within that scope.
	t.Run("composes with search instead of replacing it", func(t *testing.T) {
		filter := window
		filter.Author = "jane.doe@example.com"
		filter.Search = "nothing-here"
		_, total, _ := state.GetTasks(filter)
		assert.Equal(t, int64(0), total)

		filter.Search = "checkout"
		_, total, _ = state.GetTasks(filter)
		assert.Equal(t, int64(1), total)
	})

	t.Run("an empty author is a wildcard", func(t *testing.T) {
		_, total, _ := state.GetTasks(window)
		assert.Equal(t, int64(2), total)
	})
}

// Second-granularity timestamps make two deployments of one app in the same
// second ordinary; without a tie-breaker each backend picks a different winner.
func TestInMemoryState_GetAppSummaries_BreaksASameSecondTieById(t *testing.T) {
	older := summaryTask("checkout", models.StatusDeployedMessage, 500, 510)
	older.Id = "aaaaaaaa-0000-4000-8000-000000000001"
	newer := summaryTask("checkout", models.StatusFailedMessage, 500, 520)
	newer.Id = "bbbbbbbb-0000-4000-8000-000000000002"

	// Seeded oldest-id first, so insertion order alone would report the wrong one.
	summaries, err := seedSummaryState(older, newer).GetAppSummaries(
		models.TaskFilter{StartTime: 0, EndTime: 1000},
	)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	assert.Equal(t, models.StatusFailedMessage, summaries[0].LastStatus)
	assert.Equal(t, []string{models.StatusFailedMessage, models.StatusDeployedMessage}, summaries[0].RecentStatuses)
}
