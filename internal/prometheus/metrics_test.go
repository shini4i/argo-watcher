package prometheus

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// The result strings are spelled out rather than taken from models.Status*: they are a
// contract read outside Go — the documented PromQL, the Grafana dashboard's byName
// overrides, and the e2e metric gates — so a renamed constant must fail here.
func TestMetrics_AddDeploymentOutcome(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP deployments_total Deployments that reached a terminal state for an application ArgoCD confirmed, by outcome.
		# TYPE deployments_total counter
		deployments_total{app="test-app",result="aborted"} 1
		deployments_total{app="test-app",result="app not found"} 1
		deployments_total{app="test-app",result="cancelled"} 1
		deployments_total{app="test-app",result="deployed"} 2
		deployments_total{app="test-app",result="failed"} 1
	`

	for _, result := range []string{"deployed", "deployed", "failed", "aborted", "app not found", "cancelled"} {
		m.AddDeploymentOutcome("test-app", result)
	}

	err := testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric), "deployments_total")
	assert.NoError(t, err)
}

// The label values above are only a contract if they are what the rollout actually
// records, which is what these constants are.
func TestTerminalStatusConstantsMatchResultLabels(t *testing.T) {
	assert.Equal(t, "deployed", models.StatusDeployedMessage)
	assert.Equal(t, "failed", models.StatusFailedMessage)
	assert.Equal(t, "aborted", models.StatusAborted)
	assert.Equal(t, "app not found", models.StatusAppNotFoundMessage)
	assert.Equal(t, "cancelled", models.StatusCancelledMessage)
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

func TestMetrics_AddAcceptedDeployment(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP accepted_deployments Deployments accepted for processing, counted before ArgoCD is asked about the application.
		# TYPE accepted_deployments counter
		accepted_deployments 1
	`

	m.AddAcceptedDeployment()

	// Collected through the registry so this also fails if the collector was never
	// handed to MustRegister, in which case it would never be scraped.
	err := testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric), "accepted_deployments")
	assert.NoError(t, err)
}

func TestMetrics_AddUnconfirmedFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP unconfirmed_deployment_failures Deployments that failed before ArgoCD confirmed the application exists.
		# TYPE unconfirmed_deployment_failures counter
		unconfirmed_deployment_failures 1
	`

	m.AddUnconfirmedFailure()

	err := testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric), "unconfirmed_deployment_failures")
	assert.NoError(t, err)
}

// TestMetrics_LabellessCountersStartAtZero pins that both labelless counters are
// exported before anything increments them. The e2e soak gates on the absence of
// unconfirmed_deployment_failures to catch a rename or a dropped registration
// (test/e2e/scripts/collect.sh), which only works while a fresh process scrapes it
// as an explicit 0.
func TestMetrics_LabellessCountersStartAtZero(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewMetrics(reg)
	expectedMetric := `
		# HELP accepted_deployments Deployments accepted for processing, counted before ArgoCD is asked about the application.
		# TYPE accepted_deployments counter
		accepted_deployments 0
		# HELP unconfirmed_deployment_failures Deployments that failed before ArgoCD confirmed the application exists.
		# TYPE unconfirmed_deployment_failures counter
		unconfirmed_deployment_failures 0
	`

	err := testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric),
		"accepted_deployments", "unconfirmed_deployment_failures")
	assert.NoError(t, err)
}

// InitDeploymentOutcomes exists so PromQL range functions can see the first deployment of
// an application; the zero samples it creates are what makes that first increment a
// visible increase, so every result label must be present at zero.
func TestMetrics_InitDeploymentOutcomes(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP deployments_total Deployments that reached a terminal state for an application ArgoCD confirmed, by outcome.
		# TYPE deployments_total counter
		deployments_total{app="test-app",result="aborted"} 0
		deployments_total{app="test-app",result="app not found"} 0
		deployments_total{app="test-app",result="cancelled"} 0
		deployments_total{app="test-app",result="deployed"} 0
		deployments_total{app="test-app",result="failed"} 0
	`

	m.InitDeploymentOutcomes("test-app")

	assert.NoError(t, testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric), "deployments_total"))
}

// A task is confirmed on every deployment, so initialisation runs repeatedly against
// counters that already carry a count. It must never reset them.
func TestMetrics_InitDeploymentOutcomesKeepsExistingCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	expectedMetric := `
		# HELP deployments_total Deployments that reached a terminal state for an application ArgoCD confirmed, by outcome.
		# TYPE deployments_total counter
		deployments_total{app="test-app",result="aborted"} 0
		deployments_total{app="test-app",result="app not found"} 0
		deployments_total{app="test-app",result="cancelled"} 0
		deployments_total{app="test-app",result="deployed"} 1
		deployments_total{app="test-app",result="failed"} 0
	`

	m.InitDeploymentOutcomes("test-app")
	m.AddDeploymentOutcome("test-app", models.StatusDeployedMessage)
	m.InitDeploymentOutcomes("test-app")

	assert.NoError(t, testutil.CollectAndCompare(reg, strings.NewReader(expectedMetric), "deployments_total"))
}
