package main

import (
	"testing"

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
