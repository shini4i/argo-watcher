package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

type Metrics struct {
	failedDeployment *prometheus.GaugeVec
	processedDeployments prometheus.Counter
	argocdUnavailable prometheus.Gauge
}

func (metrics *Metrics) Init() {
	metrics.failedDeployment = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "failed_deployment",
		Help: "Per application failed deployment count before first success.",
	}, []string{"app"});

	metrics.processedDeployments = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "processed_deployments",
		Help: "The amount of deployment processed since startup.",
	});

	metrics.argocdUnavailable = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "argocd_unavailable",
		Help: "Whether ArgoCD is available for argo-watcher.",
	});
}

func (metrics *Metrics) Register() {
	log.Debug().Msg("Registering prometheus metrics...")
	prometheus.MustRegister(metrics.failedDeployment)
	prometheus.MustRegister(metrics.processedDeployments)
	prometheus.MustRegister(metrics.argocdUnavailable)
}