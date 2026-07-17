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
	ObserveRefreshDuration(app string, seconds float64)
	ObserveGitWritebackDuration(app string, seconds float64)
	ObserveGitLockWaitDuration(app string, seconds float64)
	ObserveDeploymentDuration(app string, seconds float64)
}

// Metrics contains all the prometheus collectors.
type Metrics struct {
	FailedDeployment     *prometheus.GaugeVec
	ProcessedDeployments *prometheus.CounterVec
	ArgocdUnavailable    prometheus.Gauge
	InProgressTasks      prometheus.Gauge
	RefreshDuration      *prometheus.HistogramVec
	GitWritebackDuration *prometheus.HistogramVec
	GitLockWaitDuration  *prometheus.HistogramVec
	DeploymentDuration   *prometheus.HistogramVec
}

// NewMetrics creates and registers the metrics with the provided Registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		FailedDeployment: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "failed_deployment",
			Help: "Per application failed deployment count before first success.",
		}, []string{"app"}),
		ProcessedDeployments: prometheus.NewCounterVec(prometheus.CounterOpts{
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
		RefreshDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "argocd_refresh_duration_seconds",
			Help:    "Duration of ArgoCD application refresh requests, to surface slow or stuck refreshes.",
			Buckets: prometheus.DefBuckets,
		}, []string{"app"}),
		// GitWritebackDuration times the whole write-back held under the per-repo lock:
		// the clone/commit/push cycle plus any retries and inter-attempt backoff. This is
		// the operationally meaningful number — it is how long the task blocks every other
		// write-back to the same repo. Under push contention it can approach
		// GIT_MAX_ATTEMPTS * GIT_OP_TIMEOUT plus backoff, so the buckets extend to 600s
		// (the default 10s top bucket is far too low for git ops against a large repo).
		GitWritebackDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gitops_writeback_duration_seconds",
			Help:    "Time the git write-back held the per-repo lock, covering the clone/commit/push cycle plus any retries and backoff.",
			Buckets: []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"app"}),
		// GitLockWaitDuration times how long a task waited to acquire the per-repository
		// write-back lock. Under concurrent deployments to one repo this is the dominant
		// contributor to tail latency (the last task queues behind all the others), so the
		// buckets extend to 300s.
		GitLockWaitDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gitops_lock_wait_duration_seconds",
			Help:    "Time spent waiting to acquire the per-repository git write-back lock.",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 180, 300},
		}, []string{"app"}),
		// DeploymentDuration times a successful deployment end to end: from the start of
		// rollout monitoring until the application reaches the deployed state. Only successful
		// deployments are observed — a failed deployment's wall-clock is dominated by the
		// configured timeout and would distort the distribution. Buckets span seconds (a
		// fire-and-forget commit) to minutes (a real rollout bounded by DEPLOYMENT_TIMEOUT).
		DeploymentDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "deployment_duration_seconds",
			Help:    "Wall-clock time a successful deployment took, from the start of monitoring until the app reached the deployed state.",
			Buckets: []float64{1, 2.5, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"app"}),
	}

	reg.MustRegister(m.FailedDeployment, m.ProcessedDeployments, m.ArgocdUnavailable, m.InProgressTasks, m.RefreshDuration, m.GitWritebackDuration, m.GitLockWaitDuration, m.DeploymentDuration)

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

// ObserveRefreshDuration records how long an ArgoCD refresh request took for the given app.
func (m *Metrics) ObserveRefreshDuration(app string, seconds float64) {
	m.RefreshDuration.WithLabelValues(app).Observe(seconds)
}

// ObserveGitWritebackDuration records how long the git write-back (clone, commit, push)
// took for the given app, measured while holding the per-repo lock.
func (m *Metrics) ObserveGitWritebackDuration(app string, seconds float64) {
	m.GitWritebackDuration.WithLabelValues(app).Observe(seconds)
}

// ObserveGitLockWaitDuration records how long the given app's write-back waited to acquire
// the per-repository lock.
func (m *Metrics) ObserveGitLockWaitDuration(app string, seconds float64) {
	m.GitLockWaitDuration.WithLabelValues(app).Observe(seconds)
}

// ObserveDeploymentDuration records how long a successful deployment took for the given app,
// measured from the start of rollout monitoring until the app reached the deployed state.
func (m *Metrics) ObserveDeploymentDuration(app string, seconds float64) {
	m.DeploymentDuration.WithLabelValues(app).Observe(seconds)
}
