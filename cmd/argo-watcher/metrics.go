package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

type MetricsInterface interface {
	AddFailedDeployment(app string)
	ResetFailedDeployment(app string)
	AddProcessedDeployment()
	SetArgoUnavailable(unavailable bool)
}

type Metrics struct {
	failedDeployment     *prometheus.GaugeVec
	processedDeployments prometheus.Counter
	argocdUnavailable    prometheus.Gauge
}

func (metrics *Metrics) Init() {
	metrics.failedDeployment = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "failed_deployment",
		Help: "Per application failed deployment count before first success.",
	}, []string{"app"})

	metrics.processedDeployments = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "processed_deployments",
		Help: "The amount of deployment processed since startup.",
	})

	metrics.argocdUnavailable = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "argocd_unavailable",
		Help: "Whether ArgoCD is available for argo-watcher.",
	})
}

func (metrics *Metrics) Register() {
	log.Debug().Msg("Registering prometheus metrics...")
	prometheus.MustRegister(metrics.failedDeployment)
	prometheus.MustRegister(metrics.processedDeployments)
	prometheus.MustRegister(metrics.argocdUnavailable)
}

func (metrics *Metrics) AddFailedDeployment(app string) {
	metrics.failedDeployment.With(prometheus.Labels{"app": app}).Inc()
}

func (metrics *Metrics) ResetFailedDeployment(app string) {
	metrics.failedDeployment.With(prometheus.Labels{"app": app}).Set(0)
}

func (metrics *Metrics) AddProcessedDeployment() {
	metrics.processedDeployments.Inc()
}

func (metrics *Metrics) SetArgoUnavailable(unavailable bool) {
	if unavailable {
		metrics.argocdUnavailable.Set(1)
	} else {
		metrics.argocdUnavailable.Set(0)
	}
}
