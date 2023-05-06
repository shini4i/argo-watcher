package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	s "github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
	c "github.com/shini4i/argo-watcher/internal/config"
	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
)

var (
	argoTimeout, _        = strconv.Atoi(h.GetEnv("ARGO_TIMEOUT", "0"))
	argoSyncRetryDelay    = 15 * time.Second
	retryAttempts         = uint((argoTimeout / 15) + 1)
	config                = c.GetConfig()
	argoPlannedRetryError = errors.New("planned retry")
	imagesProxy           = os.Getenv("DOCKER_IMAGES_PROXY")
)

const (
	ArgoAppSuccess = iota
	ArgoAppNotSynced
	ArgoAppNotAvailable
	ArgoAppNotHealthy
	ArgoAppFailed
)

const (
	ArgoAPIErrorTemplate        = "ArgoCD API Error: %s"
	argoUnavailableErrorMessage = "connect: connection refused"
)

type Argo struct {
	Url     string
	Token   string
	Timeout string
	client  *http.Client
	state   s.State
}

func (argo *Argo) Init() error {
	log.Debug().Msg("Initializing argo-watcher client...")

	switch state := os.Getenv("STATE_TYPE"); state {
	case "postgres":
		argo.state = &s.PostgresState{}
		argo.state.Connect()
	case "in-memory":
		argo.state = &s.InMemoryState{}
	default:
		log.Fatal().Msg("Variable STATE_TYPE must be one of [\"postgres\", \"in-memory\"]. Aborting.")
	}

	log.Debug().Msgf("Configured retry attempts per ArgoCD application status check: %d", retryAttempts)

	go argo.state.ProcessObsoleteTasks()

	skipTlsVerify, _ := strconv.ParseBool(h.GetEnv("SKIP_TLS_VERIFY", "false"))

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTlsVerify},
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}

	cookie := &http.Cookie{
		Name:  "argocd.token",
		Value: argo.Token,
	}

	argoUrl, err := url.Parse(argo.Url)
	if err != nil {
		return err
	}

	jar.SetCookies(argoUrl, []*http.Cookie{cookie})

	argoApiTimeout, _ := strconv.Atoi(argo.Timeout)
	argo.client = &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   time.Duration(argoApiTimeout) * time.Second,
	}

	log.Debug().Msgf("Timeout for ArgoCD API calls set to: %s", argo.client.Timeout)

	return nil
}

func (argo *Argo) Check() (string, error) {
	if argo == nil {
		argocdUnavailable.Set(1)
		return "", errors.New("argo-watcher is not initialized yet")
	}

	apiUrl := fmt.Sprintf("%s/api/v1/session/userinfo", argo.Url)
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := argo.client.Do(req)
	if err != nil {
		log.Error().Msg(err.Error())
		argocdUnavailable.Set(1)
		return config.StatusArgoCDUnavailableMessage, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}(resp.Body)

	var userInfo m.Userinfo
	if err = json.Unmarshal(body, &userInfo); err != nil {
		panic(err)
	}

	if argo.state.Check() && userInfo.LoggedIn {
		argocdUnavailable.Set(0)
		return "up", nil
	} else {
		// This error is misleading, need to introduce a new one
		argocdUnavailable.Set(1)
		return "down", errors.New(config.StatusArgoCDUnavailableMessage)
	}
}

func (argo *Argo) AddTask(task m.Task) (string, error) {
	status, err := argo.Check()
	if err != nil {
		return status, errors.New(err.Error())
	}

	task.Id = uuid.New().String()

	log.Info().Str("id", task.Id).Msgf("A new task was triggered. Expecting tag %s in app %s.",
		task.Images[0].Tag,
		task.App,
	)

	argo.state.Add(task)
	processedDeployments.Inc()
	go argo.waitForRollout(task)
	return task.Id, nil
}

func (argo *Argo) GetTasks(startTime float64, endTime float64, app string) m.TasksResponse {
	_, err := argo.Check()
	tasks := argo.state.GetTasks(startTime, endTime, app)

	if err != nil {
		return m.TasksResponse{
			Tasks: tasks,
			Error: err.Error(),
		}
	}

	return m.TasksResponse{
		Tasks: tasks,
	}
}

func (argo *Argo) checkAppStatus(app string) (*m.Application, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/applications/%s", argo.Url, app)
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	q := req.URL.Query()
	q.Add("refresh", "normal")
	req.URL.RawQuery = q.Encode()

	resp, err := argo.client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		var argoErrorResponse m.ArgoApiErrorResponse
		err = json.Unmarshal(body, &argoErrorResponse)
		if err != nil || argoErrorResponse.Message == "" {
			return nil, errors.New(bytes.NewBuffer(body).String())
		} else {
			return nil, errors.New(argoErrorResponse.Message)
		}
	}

	var argoApp m.Application
	err = json.Unmarshal(body, &argoApp)
	if err != nil {
		return nil, err
	}

	return &argoApp, nil
}

func (argo *Argo) checkWithRetry(task m.Task) (int, error) {
	var status int

	err := retry.Do(
		func() error {
			app, err := argo.checkAppStatus(task.App)

			if err != nil {
				status = ArgoAppFailed
				return err
			}

			for _, image := range task.Images {
				expected := fmt.Sprintf("%s:%s", image.Image, image.Tag)
				if !h.ImageContains(app.Status.Summary.Images, expected, imagesProxy) {
					log.Debug().Str("id", task.Id).Msgf("%s is not available yet", expected)
					status = ArgoAppNotAvailable
					return argoPlannedRetryError
				}
			}

			if app.Status.Sync.Status != "Synced" {
				log.Debug().Str("id", task.Id).Msgf("%s is not synced yet", task.App)
				status = ArgoAppNotSynced
				return argoPlannedRetryError
			}

			if app.Status.Health.Status != "Healthy" {
				log.Debug().Str("id", task.Id).Msgf("%s is not healthy yet", task.App)
				status = ArgoAppNotHealthy
				return argoPlannedRetryError
			}

			status = ArgoAppSuccess
			return nil
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(argoSyncRetryDelay),
		retry.Attempts(retryAttempts),
		retry.RetryIf(func(err error) bool {
			return errors.Is(err, argoPlannedRetryError)
		}),
		retry.LastErrorOnly(true),
	)

	return status, err
}

func (argo *Argo) waitForRollout(task m.Task) {
	// continuously check for application status change
	status, err := argo.checkWithRetry(task)

	// application synced successfully
	if status == ArgoAppSuccess {
		argo.handleDeploymentSuccess(task)
		return
	}

	// we had some unexpected error with ArgoCD API
	if status == ArgoAppFailed {
		argo.handleArgoAPIFailure(task, err)
		return
	}

	// fetch application details
	app, err := argo.checkAppStatus(task.App)

	// define default message
	const defaultErrorMessage string = "could not retrieve details"
	// handle application sync failure
	switch status {
	// not all images were deployed to the application
	case ArgoAppNotAvailable:
		// show list of missing images
		var message string
		// define details
		if err != nil {
			message = defaultErrorMessage
		} else {
			message = "List of current images (last app check):\n"
			message += "\t" + strings.Join(app.Status.Summary.Images, "\n\t") + "\n\n"
			message += "List of expected images:\n"
			message += "\t" + strings.Join(task.ListImages(), "\n\t")
		}
		// handle error
		argo.handleAppNotAvailable(task, errors.New(message))
	// application sync status wasn't valid
	case ArgoAppNotSynced:
		// display sync status and last sync message
		var message string
		// define details
		if err != nil {
			message = defaultErrorMessage
		} else {
			message = "App status \"" + app.Status.OperationState.Phase + "\"\n"
			message += "App message \"" + app.Status.OperationState.Message + "\"\n"
			message += "Resources:\n"
			message += "\t" + strings.Join(app.ListSyncResultResources(), "\n\t")
		}
		// handle error
		argo.handleAppOutOfSync(task, errors.New(message))
	// application is not in a healthy status
	case ArgoAppNotHealthy:
		// display current health of pods
		var message string
		// define details
		if err != nil {
			message = defaultErrorMessage
		} else {
			message = "App sync status \"" + app.Status.Sync.Status + "\"\n"
			message += "App health status \"" + app.Status.Health.Status + "\"\n"
			message += "Resources:\n"
			message += "\t" + strings.Join(app.ListUnhealthyResources(), "\n\t")
		}
		// handle error
		argo.handleAppNotHealthy(task, errors.New(message))
	// handle unexpected status
	default:
		argo.handleDeploymentUnexpectedStatus(task, errors.New(fmt.Sprintf("Received unexpected status \"%d\"", status)))
	}
}

func (argo *Argo) handleArgoAPIFailure(task m.Task, err error) {
	// notify user that app wasn't found
	appNotFoundError := fmt.Sprintf("applications.argoproj.io \"%s\" not found", task.App)
	if strings.Contains(err.Error(), appNotFoundError) {
		argo.handleAppNotFound(task, err)
		return
	}
	// notify user that ArgoCD API isn't available
	if strings.Contains(err.Error(), argoUnavailableErrorMessage) {
		argo.handleArgoUnavailable(task, err)
		return
	}

	// notify of unexpected error
	argo.handleDeploymentFailed(task, err)
	return
}

func (argo *Argo) handleAppNotFound(task m.Task, err error) {
	log.Info().Str("id", task.Id).Msgf("Application %s does not exist.", task.App)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusAppNotFoundMessage, reason)
}

func (argo *Argo) handleArgoUnavailable(task m.Task, err error) {
	log.Error().Str("id", task.Id).Msg("ArgoCD is not available. Aborting.")
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	argo.state.SetTaskStatus(task.Id, "aborted", reason)
}

func (argo *Argo) handleDeploymentFailed(task m.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Aborting with error: %s", err)
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleDeploymentSuccess(task m.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the excepted version.")
	failedDeployment.With(prometheus.Labels{"app": task.App}).Set(0)
	argo.state.SetTaskStatus(task.Id, "deployed", "")
}

func (argo *Argo) handleAppNotAvailable(task m.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application not available\n%s", err.Error())
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application not available\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleAppNotHealthy(task m.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application not healthy\n%s", err.Error())
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application not healthy\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleAppOutOfSync(task m.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application out of sync\n%s", err.Error())
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application out of sync\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleDeploymentUnexpectedStatus(task m.Task, err error) {
	log.Error().Str("id", task.Id).Msg("Deployment timed out with unexpected status. Aborting.")
	log.Error().Str("id", task.Id).Msgf("Deployment error\n%s", err.Error())
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Deployment timeout\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) GetAppList() []string {
	return argo.state.GetAppList()
}

func (argo *Argo) SimpleHealthCheck() bool {
	return argo.state.Check()
}
