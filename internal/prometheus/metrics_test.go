package prometheus

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_AddProcessedDeployment(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP processed_deployments The amount of deployment processed since startup.
		# TYPE processed_deployments counter
		processed_deployments{app="test-app"} 1
	`

	m.AddProcessedDeployment("test-app")

	err := testutil.CollectAndCompare(m.ProcessedDeployments, strings.NewReader(expectedMetric))
	assert.NoError(t, err)
}

func TestMetrics_AddFailedDeployment(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	appName := "test-app"
	expectedMetric := `
		# HELP failed_deployment Per application failed deployment count before first success.
		# TYPE failed_deployment gauge
		failed_deployment{app="test-app"} 1
	`

	m.AddFailedDeployment(appName)

	err := testutil.CollectAndCompare(m.FailedDeployment, strings.NewReader(expectedMetric))
	assert.NoError(t, err)
}

func TestMetrics_ResetFailedDeployment(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	appName := "test-app"
	m.AddFailedDeployment(appName) // Set a value first

	expectedMetric := `
		# HELP failed_deployment Per application failed deployment count before first success.
		# TYPE failed_deployment gauge
		failed_deployment{app="test-app"} 0
	`

	m.ResetFailedDeployment(appName)

	err := testutil.CollectAndCompare(m.FailedDeployment, strings.NewReader(expectedMetric))
	assert.NoError(t, err)
}

func TestMetrics_SetArgoUnavailable(t *testing.T) {
	testCases := []struct {
		name          string
		unavailable   bool
		expectedValue float64
	}{
		{"Set to unavailable", true, 1},
		{"Set to available", false, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m := NewMetrics(reg)

			m.SetArgoUnavailable(tc.unavailable)

			assert.Equal(t, tc.expectedValue, testutil.ToFloat64(m.ArgocdUnavailable))
		})
	}
}

func TestMetrics_SetStateUnavailable(t *testing.T) {
	testCases := []struct {
		name          string
		unavailable   bool
		expectedValue float64
	}{
		{"Set to unavailable", true, 1},
		{"Set to available", false, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m := NewMetrics(reg)

			m.SetStateUnavailable(tc.unavailable)

			assert.Equal(t, tc.expectedValue, testutil.ToFloat64(m.StateUnavailable))
		})
	}
}

func histogramSampleForApp(t *testing.T, vec *prometheus.HistogramVec, app string) (count uint64, sum float64) {
	t.Helper()
	obs, err := vec.GetMetricWithLabelValues(app)
	require.NoError(t, err)
	var metric dto.Metric
	require.NoError(t, obs.(prometheus.Metric).Write(&metric))
	return metric.GetHistogram().GetSampleCount(), metric.GetHistogram().GetSampleSum()
}

func TestMetrics_AddSkippedWriteback(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP gitops_writeback_skipped_unvalidated Write-backs skipped because a task for a watcher-managed application presented no valid credential.
		# TYPE gitops_writeback_skipped_unvalidated counter
		gitops_writeback_skipped_unvalidated{app="test-app"} 1
	`

	m.AddSkippedWriteback("test-app")

	// Collected through the registry rather than the collector, so this also fails if the
	// collector is never handed to MustRegister — in which case it would be correct and
	// simply never scraped.
	err := testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric), "gitops_writeback_skipped_unvalidated")
	assert.NoError(t, err)
}

func TestMetrics_ObserveGitWritebackDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveGitWritebackDuration("test-app", 3.5)

	count, sum := histogramSampleForApp(t, m.GitWritebackDuration, "test-app")
	assert.Equal(t, uint64(1), count)
	assert.Equal(t, 3.5, sum)
}

func TestMetrics_ObserveGitLockWaitDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveGitLockWaitDuration("test-app", 12)

	count, sum := histogramSampleForApp(t, m.GitLockWaitDuration, "test-app")
	assert.Equal(t, uint64(1), count)
	assert.Equal(t, float64(12), sum)
}

func TestMetrics_ObserveGitBatchSize(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveGitBatchSize(3)

	// GitBatchSize is a plain Histogram (no per-app label), so read it directly.
	var metric dto.Metric
	require.NoError(t, m.GitBatchSize.Write(&metric))
	assert.Equal(t, uint64(1), metric.GetHistogram().GetSampleCount())
	assert.Equal(t, float64(3), metric.GetHistogram().GetSampleSum())
}

func TestMetrics_ObserveDeploymentDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveDeploymentDuration("test-app", 42)

	count, sum := histogramSampleForApp(t, m.DeploymentDuration, "test-app")
	assert.Equal(t, uint64(1), count)
	assert.Equal(t, float64(42), sum)
}

func TestMetrics_InProgressTasks(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	assert.Equal(t, float64(0), testutil.ToFloat64(m.InProgressTasks))

	m.AddInProgressTask()
	assert.Equal(t, float64(1), testutil.ToFloat64(m.InProgressTasks))

	m.RemoveInProgressTask()
	assert.Equal(t, float64(0), testutil.ToFloat64(m.InProgressTasks))
}
