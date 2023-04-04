package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/conf"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var version = "local"

var (
	failedDeployment = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "failed_deployment",
		Help: "Per application failed deployment count before first success.",
	}, []string{"app"})
	processedDeployments = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "processed_deployments",
		Help: "The amount of deployment processed since startup.",
	})
	argocdUnavailable = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "argocd_unavailable",
		Help: "Whether ArgoCD is available for argo-watcher.",
	})
)

// getVersion godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/version [get]
func getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, version)
}

func prometheusHandler() gin.HandlerFunc {
	ph := promhttp.Handler()

	return func(c *gin.Context) {
		ph.ServeHTTP(c.Writer, c.Request)
	}
}

func prometheusRegisterMetrics() {
	log.Debug().Msg("Registering prometheus metrics...")
	prometheus.MustRegister(failedDeployment)
	prometheus.MustRegister(processedDeployments)
	prometheus.MustRegister(argocdUnavailable)
}

// reference: https://www.alexedwards.net/blog/organising-database-access
type Env struct {
	// environment configurations
	config *conf.Container
	// argo client
	client *Argo
}

func main() {
	// initialize config
	config, err := conf.InitConfig()
	if err != nil {
		log.Error().Msgf("Couldn't initialize config. Error: %s", err)
		os.Exit(1)
	}

	// initialize logs
	logLevel, err := zerolog.ParseLevel(config.LogLevel)
	if err != nil {
		log.Warn().Msgf("Couldn't parse log level. Got the following error: %s", err)
		logLevel = zerolog.InfoLevel
	}

	log.Debug().Msgf("Setting log level to %s", logLevel)
	zerolog.SetGlobalLevel(logLevel)

	// initialize argo client
	client := Argo{
		Url:     config.ArgoUrl,
		Token:   config.ArgoToken,
		Timeout: config.ArgoApiTimeout,
	}

	if err := client.InitArgo(config); err != nil {
		log.Error().Msgf("Couldn't initialize the client. Got the following error: %s", err)
		os.Exit(1)
	}

	// create environment
	env := &Env{config: config, client: &client}

	// setup prometheus metrics
	prometheusRegisterMetrics()

	// start the server
	log.Info().Msg("Starting web server")
	router := createRouter(env)
	routerBind := fmt.Sprintf("%s:%s", config.Host, config.Port)
	log.Debug().Msgf("Listening on %s", routerBind)
	if err := router.Run(routerBind); err != nil {
		panic(err)
	}
}
