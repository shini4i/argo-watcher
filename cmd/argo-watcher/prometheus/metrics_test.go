package prometheus

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMetrics_AddProcessedDeployment(t *testing.T) {
	// Arrange
	m := NewMetrics()
	expectedMetric := `
		# HELP processed_deployments The amount of deployment processed since startup.
		# TYPE processed_deployments counter
		processed_deployments 1
	`

	// Act
	m.AddProcessedDeployment("test-app")

	// Assert
	err := testutil.CollectAndCompare(m.ProcessedDeployments, strings.NewReader(expectedMetric))
	assert.NoError(t, err)
}

func TestMetrics_AddFailedDeployment(t *testing.T) {
	// Arrange
	m := NewMetrics()
	appName := "test-app"
	expectedMetric := `
		# HELP failed_deployment Per application failed deployment count before first success.
		# TYPE failed_deployment gauge
		failed_deployment{app="test-app"} 1
	`

	// Act
	m.AddFailedDeployment(appName)

	// Assert
	err := testutil.CollectAndCompare(m.FailedDeployment, strings.NewReader(expectedMetric))
	assert.NoError(t, err)
}

func TestMetrics_ResetFailedDeployment(t *testing.T) {
	// Arrange
	m := NewMetrics()
	appName := "test-app"
	m.AddFailedDeployment(appName) // Set a value first

	expectedMetric := `
		# HELP failed_deployment Per application failed deployment count before first success.
		# TYPE failed_deployment gauge
		failed_deployment{app="test-app"} 0
	`

	// Act
	m.ResetFailedDeployment(appName)

	// Assert
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
			// Arrange
			m := NewMetrics()

			// Act
			m.SetArgoUnavailable(tc.unavailable)

			// Assert
			assert.Equal(t, tc.expectedValue, testutil.ToFloat64(m.ArgocdUnavailable))
		})
	}
}

func TestMetrics_InProgressTasks(t *testing.T) {
	// Arrange
	m := NewMetrics()

	// Assert initial state
	assert.Equal(t, float64(0), testutil.ToFloat64(m.InProgressTasks))

	// Act: Add a task
	m.AddInProgressTask()
	assert.Equal(t, float64(1), testutil.ToFloat64(m.InProgressTasks))

	// Act: Remove a task
	m.RemoveInProgressTask()
	assert.Equal(t, float64(0), testutil.ToFloat64(m.InProgressTasks))
}
