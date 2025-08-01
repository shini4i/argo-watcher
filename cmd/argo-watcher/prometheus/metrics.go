package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsInterface defines the interface for the metrics service. This is required
// for dependency injection and mocking in tests.
type MetricsInterface interface {
	AddProcessedDeployment(app string)
	AddFailedDeployment(app string)
	ResetFailedDeployment(app string)
	SetArgoUnavailable(unavailable bool)
	AddInProgressTask()
	RemoveInProgressTask()
}

// Metrics contains all the prometheus collectors.
type Metrics struct {
	FailedDeployment     *prometheus.GaugeVec
	ProcessedDeployments *prometheus.GaugeVec
	ArgocdUnavailable    prometheus.Gauge
	InProgressTasks      prometheus.Gauge
}

// NewMetrics creates and registers the metrics with the provided Registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		FailedDeployment: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "failed_deployment",
			Help: "Per application failed deployment count before first success.",
		}, []string{"app"}),
		ProcessedDeployments: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "processed_deployments",
			Help: "The amount of deployment processed since startup.",
		}, []string{"app"}),
		ArgocdUnavailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "argocd_unavailable",
			Help: "Whether ArgoCD is available for argo-watcher.",
		}),
		InProgressTasks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "in_progress_tasks",
			Help: "The number of tasks currently in progress.",
		}),
	}

	reg.MustRegister(m.FailedDeployment, m.ProcessedDeployments, m.ArgocdUnavailable, m.InProgressTasks)

	return m
}

// AddProcessedDeployment increments the ProcessedDeployments counter.
func (m *Metrics) AddProcessedDeployment(app string) {
	m.ProcessedDeployments.WithLabelValues(app).Inc()
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
