package prometheus

import (
	"testing"

	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMetricsValuesChange(t *testing.T) {
	metrics := &Metrics{}
	metrics.Init()

	app := "testApp"

	t.Run("AddFailedDeployment", func(t *testing.T) {
		// Call the method to test
		metrics.AddFailedDeployment(app)

		// Get the current value of the metric for the app
		metric := testutil.ToFloat64(metrics.failedDeployment.With(prometheus.Labels{"app": app}))

		// Check if the metric was incremented
		assert.Equal(t, 1.0, metric)
	})

	t.Run("ResetFailedDeployment", func(t *testing.T) {
		// Call the method to test
		metrics.ResetFailedDeployment(app)

		// Get the current value of the metric for the app
		metric := testutil.ToFloat64(metrics.failedDeployment.With(prometheus.Labels{"app": app}))

		// Check if the metric was reset
		assert.Equal(t, 0.0, metric)
	})

	t.Run("IncrementProcessedDeployments", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			metrics.AddProcessedDeployment()
		}

		// Get the current value of the metric for the app
		metric := testutil.ToFloat64(metrics.processedDeployments)

		// Check if the metric was incremented
		assert.Equal(t, 10.0, metric)
	})
}

func TestSetArgoUnavailable(t *testing.T) {
	metrics := &Metrics{}
	metrics.Init()

	t.Run("SetArgoUnavailable to true", func(t *testing.T) {
		// Call the method to test
		metrics.SetArgoUnavailable(true)

		// Get the current value of the metric
		metric := testutil.ToFloat64(metrics.argocdUnavailable)

		// Check if the metric was set to 1
		assert.Equal(t, 1.0, metric)
	})

	t.Run("SetArgoUnavailable to false", func(t *testing.T) {
		// Call the method to test
		metrics.SetArgoUnavailable(false)

		// Get the current value of the metric
		metric := testutil.ToFloat64(metrics.argocdUnavailable)

		// Check if the metric was set to 0
		assert.Equal(t, 0.0, metric)
	})
}

func TestRegister(t *testing.T) {
	metrics := &Metrics{}
	metrics.Init()

	// Call the method to test
	metrics.Register()

	// adding a failed deployment to check if the metric was registered
	metrics.AddFailedDeployment("testApp")

	// Check if the metrics were registered
	assert.True(t, testMetricRegistered("failed_deployment"))
	assert.True(t, testMetricRegistered("processed_deployments"))
	assert.True(t, testMetricRegistered("argocd_unavailable"))
}

// Helper function to check if a metric is registered.
func testMetricRegistered(metricName string) bool {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		log.Error().Msgf("Error gathering metrics: %v", err)
		return false
	}

	for _, m := range metricFamilies {
		if m.GetName() == metricName {
			return true
		}
	}

	return false
}

func TestInProgressTasks(t *testing.T) {
	metrics := &Metrics{}
	metrics.Init()

	t.Run("AddInProgressTask", func(t *testing.T) {
		// Call the method to test
		metrics.AddInProgressTask()

		// Get the current value of the metric
		metric := testutil.ToFloat64(metrics.inProgressTasks)

		// Check if the metric was incremented
		assert.Equal(t, 1.0, metric)
	})

	t.Run("RemoveInProgressTask", func(t *testing.T) {
		// Call the method to test
		metrics.RemoveInProgressTask()

		// Get the current value of the metric
		metric := testutil.ToFloat64(metrics.inProgressTasks)

		// Check if the metric was decremented
		assert.Equal(t, 0.0, metric)
	})
}
