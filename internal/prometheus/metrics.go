package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsInterface defines the interface for the metrics service. This is required
// for dependency injection and mocking in tests.
type MetricsInterface interface {
	AddAcceptedDeployment()
	AddDeploymentOutcome(app, result string)
	AddUnconfirmedFailure()
	AddFailedDeployment(app string)
	ResetFailedDeployment(app string)
	SetArgoUnavailable(unavailable bool)
	SetStateUnavailable(unavailable bool)
	AddInProgressTask()
	RemoveInProgressTask()
	ObserveRefreshDuration(app string, seconds float64)
	ObserveGitWritebackDuration(app string, seconds float64)
	ObserveGitLockWaitDuration(app string, seconds float64)
	ObserveDeploymentDuration(app string, seconds float64)
	ObserveGitBatchSize(size int)
	AddUnauthenticatedRead(path, app string)
	AddSkippedWriteback(app string)
}

type Metrics struct {
	FailedDeployment     *prometheus.GaugeVec
	DeploymentsTotal     *prometheus.CounterVec
	AcceptedDeployments  prometheus.Counter
	UnconfirmedFailures  prometheus.Counter
	ArgocdUnavailable    prometheus.Gauge
	StateUnavailable     prometheus.Gauge
	InProgressTasks      prometheus.Gauge
	RefreshDuration      *prometheus.HistogramVec
	GitWritebackDuration *prometheus.HistogramVec
	GitLockWaitDuration  *prometheus.HistogramVec
	DeploymentDuration   *prometheus.HistogramVec
	GitBatchSize         prometheus.Histogram
	UnauthenticatedReads *prometheus.CounterVec
	SkippedWritebacks    *prometheus.CounterVec
}

// NewMetrics registers the collectors with the provided Registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		// FailedDeployment records failures of an application ArgoCD confirmed; a deployment
		// that failed before that is counted by UnconfirmedFailures instead, so the gauge is
		// only ever raised under a name a later success can reset.
		FailedDeployment: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "failed_deployment",
			Help: "Per application failed deployment count before first success.",
		}, []string{"app"}),
		// Counted once per deployment, only for an application ArgoCD confirmed — which is
		// what makes the app name safe as a label (issue #552). See UnconfirmedFailures.
		DeploymentsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "deployments_total",
			Help: "Deployments that reached a terminal state for an application ArgoCD confirmed, by outcome.",
		}, []string{"app", "result"}),
		// AcceptedDeployments counts every deployment accepted, recorded when the task is
		// created and therefore before ArgoCD is asked anything. It carries no app label
		// because at that point the name is only what the submission claimed, and labelling
		// it would let anyone reaching the endpoint mint a permanent series.
		AcceptedDeployments: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "accepted_deployments",
			Help: "Deployments accepted for processing, counted before ArgoCD is asked about the application.",
		}),
		// UnconfirmedFailures counts deployments that ended before ArgoCD confirmed the
		// application — a missing or misspelled name, or ArgoCD unreachable — and resumed
		// tasks whose window had already elapsed. Labelless for the same reason as
		// AcceptedDeployments; the per-app counterpart is FailedDeployment.
		UnconfirmedFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "unconfirmed_deployment_failures",
			Help: "Deployments that failed before ArgoCD confirmed the application exists.",
		}),
		ArgocdUnavailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "argocd_unavailable",
			Help: "Whether the ArgoCD API is unreachable (1) or reachable (0) for argo-watcher. Independent of the state backend (see state_unavailable).",
		}),
		StateUnavailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "state_unavailable",
			Help: "Whether argo-watcher's state backend (database) is unreachable (1) or reachable (0). Independent of ArgoCD (see argocd_unavailable).",
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
		// GitBatchSize records how many applications were coalesced into a single
		// batch write-back flush (one clone + one push). It is only observed when
		// batch mode is enabled; a distribution skewed toward 1 means little
		// coalescing is happening (low contention), while higher values show the
		// batcher collapsing concurrent write-backs to the same repo.
		GitBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "gitops_batch_size",
			Help:    "Number of applications committed in a single batch write-back flush.",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100},
		}),
		// UnauthenticatedReads is the migration signal for closing the read endpoints
		// left open on purpose (see router.go): it keeps rising while pipelines run a
		// client that sends no credential on GETs, and reaching zero is the evidence
		// that requiring auth there is safe. The app label names who still has to
		// upgrade; it is "unknown" when the read did not resolve to a task.
		UnauthenticatedReads: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "unauthenticated_reads",
			Help: "Reads served without a credential on the deliberately open read endpoints while OIDC auth was enabled.",
		}, []string{"path", "app"}),
		// SkippedWritebacks counts deployments of an app annotated for write-back whose
		// task carried no valid credential, so the commit never happened. The deployment
		// that follows normally fails blaming the image or the timeout, and reports success
		// only under argo-watcher/fire-and-forget — which is why this counter, not the
		// deployment status, is the signal. Any value above zero is a misconfiguration
		// worth alerting on; the usual cause is a credential dropped by a redirect that
		// leaves the host the client was pointed at.
		SkippedWritebacks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gitops_writeback_skipped_unvalidated",
			Help: "Write-backs skipped because a task for a watcher-managed application presented no valid credential.",
		}, []string{"app"}),
	}

	reg.MustRegister(m.FailedDeployment, m.DeploymentsTotal, m.AcceptedDeployments, m.UnconfirmedFailures, m.ArgocdUnavailable, m.StateUnavailable, m.InProgressTasks, m.RefreshDuration, m.GitWritebackDuration, m.GitLockWaitDuration, m.DeploymentDuration, m.GitBatchSize, m.UnauthenticatedReads, m.SkippedWritebacks)

	return m
}

// AddDeploymentOutcome increments DeploymentsTotal for app under the terminal status
// result. Call once per deployment, and only for a confirmed application; see that field.
func (m *Metrics) AddDeploymentOutcome(app, result string) {
	m.DeploymentsTotal.WithLabelValues(app, result).Inc()
}

// AddAcceptedDeployment increments AcceptedDeployments; see that field for why it carries
// no app label.
func (m *Metrics) AddAcceptedDeployment() {
	m.AcceptedDeployments.Inc()
}

// AddUnconfirmedFailure increments UnconfirmedFailures; see that field for which failures
// belong here rather than in FailedDeployment.
func (m *Metrics) AddUnconfirmedFailure() {
	m.UnconfirmedFailures.Inc()
}

func (m *Metrics) AddFailedDeployment(app string) {
	m.FailedDeployment.WithLabelValues(app).Inc()
}

func (m *Metrics) ResetFailedDeployment(app string) {
	m.FailedDeployment.WithLabelValues(app).Set(0)
}

func (m *Metrics) SetArgoUnavailable(unavailable bool) {
	if unavailable {
		m.ArgocdUnavailable.Set(1)
	} else {
		m.ArgocdUnavailable.Set(0)
	}
}

// SetStateUnavailable sets the StateUnavailable gauge.
func (m *Metrics) SetStateUnavailable(unavailable bool) {
	if unavailable {
		m.StateUnavailable.Set(1)
	} else {
		m.StateUnavailable.Set(0)
	}
}

func (m *Metrics) AddInProgressTask() {
	m.InProgressTasks.Inc()
}

func (m *Metrics) RemoveInProgressTask() {
	m.InProgressTasks.Dec()
}

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

// ObserveGitBatchSize records how many applications were coalesced into one flush.
func (m *Metrics) ObserveGitBatchSize(size int) {
	m.GitBatchSize.Observe(float64(size))
}

// AddUnauthenticatedRead increments the UnauthenticatedReads counter for the given
// request path and application. Callers pass "unknown" as app when the read did not
// resolve to a task.
func (m *Metrics) AddUnauthenticatedRead(path, app string) {
	m.UnauthenticatedReads.WithLabelValues(path, app).Inc()
}

// AddSkippedWriteback increments SkippedWritebacks for app; see that field for what a
// non-zero value means.
func (m *Metrics) AddSkippedWriteback(app string) {
	m.SkippedWritebacks.WithLabelValues(app).Inc()
}
