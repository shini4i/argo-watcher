package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics contains all the prometheus collectors and a registry.
type Metrics struct {
	registry             *prometheus.Registry
	FailedDeployment     *prometheus.GaugeVec
	ProcessedDeployments prometheus.Counter
	ArgocdUnavailable    prometheus.Gauge
	InProgressTasks      prometheus.Gauge
}

// NewMetrics creates a new Metrics instance with its own registry and collectors.
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		FailedDeployment: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "failed_deployment",
			Help: "Per application failed deployment count before first success.",
		}, []string{"app"}),
		ProcessedDeployments: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "processed_deployments",
			Help: "The amount of deployment processed since startup.",
		}),
		ArgocdUnavailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "argocd_unavailable",
			Help: "Whether ArgoCD is available for argo-watcher.",
		}),
		InProgressTasks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "in_progress_tasks",
			Help: "The number of tasks currently in progress.",
		}),
	}

	m.register()

	return m
}

// register registers the metrics with the instance's private registry.
func (m *Metrics) register() {
	m.registry.MustRegister(m.FailedDeployment)
	m.registry.MustRegister(m.ProcessedDeployments)
	m.registry.MustRegister(m.ArgocdUnavailable)
	m.registry.MustRegister(m.InProgressTasks)
}

// GetRegistry returns the private registry of the Metrics instance.
func (m *Metrics) GetRegistry() *prometheus.Registry {
	return m.registry
}

// AddProcessedDeployment increments the ProcessedDeployments counter.
func (m *Metrics) AddProcessedDeployment(app string) {
	m.ProcessedDeployments.Inc()
}

// AddFailedDeployment increments the FailedDeployment gauge for the given app.
func (m *Metrics) AddFailedDeployment(app string) {
	m.FailedDeployment.WithLabelValues(app).Inc()
}

// ResetFailedDeployment resets the FailedDeployment gauge for the given app.
func (m *Metrics) ResetFailedDeployment(app string) {
	m.FailedDeployment.WithLabelValues(app).Set(0)
}

// SetArgoUnavailable sets the ArgocdUnavailable gauge.
func (m *Metrics) SetArgoUnavailable(unavailable bool) {
	if unavailable {
		m.ArgocdUnavailable.Set(1)
	} else {
		m.ArgocdUnavailable.Set(0)
	}
}

// AddInProgressTask increments the InProgressTasks gauge.
func (m *Metrics) AddInProgressTask() {
	m.InProgressTasks.Inc()
}

// RemoveInProgressTask decrements the InProgressTasks gauge.
func (m *Metrics) RemoveInProgressTask() {
	m.InProgressTasks.Dec()
}
