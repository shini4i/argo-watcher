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
	"github.com/romana/rlog"
	"io"
	"net/http"
	"net/http/cookiejar"
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
	argoTimeout, _     = strconv.Atoi(os.Getenv("ARGO_TIMEOUT"))
	argoApiTimeout, _  = strconv.Atoi(os.Getenv("ARGO_API_TIMEOUT"))
	argoSyncRetryDelay = 15 * time.Second
	retryAttempts      = uint((argoTimeout / 15) + 1)
	argoAuthRetryDelay = 15 * time.Second
	config             = c.GetConfig()
)

const (
	ArgoAppSuccess = iota
	ArgoAppNotSynced
	ArgoAppNotAvailable
	ArgoAppNotHealthy
	ArgoAppFailed
)

type Argo struct {
	Url      string
	User     string
	Password string
	client   *http.Client
	state    s.State
}

func (argo *Argo) Init() {
	type argoAuth struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	switch state := os.Getenv("STATE_TYPE"); state {
	case "postgres":
		argo.state = &s.PostgresState{}
		argo.state.Connect()
	case "in-memory":
		argo.state = &s.InMemoryState{}
	default:
		rlog.Critical("Variable STATE_TYPE must be one of [\"postgres\", \"in-memory\"]. Aborting.")
		os.Exit(1)
	}

	rlog.Infof("Configured retry attempts per ArgoCD application status check: %d", retryAttempts)

	go argo.state.ProcessObsoleteTasks()

	body, err := json.Marshal(argoAuth{
		Username: argo.User,
		Password: argo.Password,
	})
	if err != nil {
		panic(err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}

	url := fmt.Sprintf("%s/api/v1/session", argo.Url)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	skipTlsVerify, _ := strconv.ParseBool(h.GetEnv("SKIP_TLS_VERIFY", "false"))

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTlsVerify},
	}

	argo.client = &http.Client{
		Jar:       jar,
		Transport: transport,
	}
	if argoApiTimeout > 0 {
		argo.client.Timeout = time.Duration(argoApiTimeout) * time.Second
		rlog.Debugf("Timeout for ArgoCD API calls set to: %s", argo.client.Timeout)
	} else {
		rlog.Debugf("Timeout for ArgoCD API is set to default")
	}

	err = retry.Do(
		func() error {
			resp, err := argo.client.Do(req)
			if err != nil {
				argocdUnavailable.Set(1)
				return errors.New(fmt.Sprintf("Couldn't establish connection to ArgoCD, got the following error: %s", err))
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					panic(err)
				}
			}(resp.Body)

			if resp.StatusCode != 200 {
				return errors.New(fmt.Sprintf("ArgoCD authentication error: %s", bytes.NewBuffer(body).String()))
			}

			rlog.Debug("Authenticated into ArgoCD API")
			return nil
		},
		retry.Attempts(0),
		retry.Delay(argoAuthRetryDelay),
		retry.OnRetry(func(n uint, err error) {
			rlog.Error(err)
			rlog.Infof("Retrying ArgoCD authentication attempt in %s", argoAuthRetryDelay.String())
		}),
	)

	if err != nil {
		panic(err)
	}
}

func (argo *Argo) Check() (string, error) {
	if argo == nil {
		argocdUnavailable.Set(1)
		return "", errors.New("argo-watcher is not initialized yet")
	}

	url := fmt.Sprintf("%s/api/v1/session/userinfo", argo.Url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := argo.client.Do(req)
	if err != nil {
		rlog.Error(err)
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
			rlog.Error(err)
		}
	}(resp.Body)

	var userInfo m.Userinfo
	err = json.Unmarshal(body, &userInfo)
	if err != nil {
		panic(err)
	}

	if userInfo.LoggedIn && argo.state.Check() {
		argocdUnavailable.Set(0)
		return "up", nil
	} else {
		argocdUnavailable.Set(1)
		return "down", nil
	}
}

func (argo *Argo) AddTask(task m.Task) (string, error) {
	status, err := argo.Check()
	if err != nil {
		return status, errors.New(err.Error())
	}

	task.Id = uuid.New().String()

	rlog.Infof("[%s] A new task was triggered. Expecting tag %s in app %s.",
		task.Id,
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

func (argo *Argo) GetTaskStatus(id string) string {
	return argo.state.GetTaskStatus(id)
}

func (argo *Argo) checkAppStatus(app string) (*m.Application, error) {
	url := fmt.Sprintf("%s/api/v1/applications/%s", argo.Url, app)
	req, err := http.NewRequest("GET", url, nil)
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
			rlog.Error(err)
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
				if !h.Contains(app.Status.Summary.Images, expected) {
					rlog.Debugf("[%s] %s is not available yet", task.Id, expected)
					status = ArgoAppNotAvailable
					return errors.New("retry")
				}
			}

			if app.Status.Sync.Status != "Synced" {
				rlog.Debugf("[%s] %s is not synced yet", task.Id, task.App)
				status = ArgoAppNotSynced
				return errors.New("retry")
			}

			if app.Status.Health.Status != "Healthy" {
				rlog.Debugf("[%s] %s is not healthy yet", task.Id, task.App)
				status = ArgoAppNotHealthy
				return errors.New("retry")
			}

			status = ArgoAppSuccess
			return nil
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(argoSyncRetryDelay),
		retry.Attempts(retryAttempts),
		retry.RetryIf(func(err error) bool {
			return err.Error() == "retry"
		}),
		retry.LastErrorOnly(true),
	)

	return status, err
}

func (argo *Argo) waitForRollout(task m.Task) {
	// continuously check for application status change
	status, err := argo.checkWithRetry(task)

	// we had some unexpected error with ArgoCD API
	if status == ArgoAppFailed {
		// notify user that app wasn't found
		appNotFoundError := fmt.Sprintf("applications.argoproj.io \"%s\" not found", task.App)
		if strings.Contains(err.Error(), appNotFoundError) {
			argo.handleAppNotFound(task, err)
			return
		}
		// notify user that ArgoCD API isn't available
		argoUnavailableError := "connect: connection refused"
		if strings.Contains(err.Error(), argoUnavailableError) {
			argo.handleArgoUnavailable(task, err)
			return
		}

		// notify of unexpected error
		argo.handleDeploymentFailed(task, err)
	}

	// not all images were deployed to the application
	if status == ArgoAppNotAvailable {
		// show list of missing images
		app, err := argo.checkAppStatus(task.App)
		var message string

		if err != nil {
			message = "could not retrieve details"
		} else {
			message = "List of current images (last app check):\n"
			message += "\t" + strings.Join(app.Status.Summary.Images, "\n\t") + "\n\n"
			message += "List of expected images:\n"
			message += "\t" + strings.Join(task.ListImages(), "\n\t")
		}

		// handle error
		argo.handleAppNotAvailable(task, errors.New(message))
	}

	// application sync status wasn't valid
	if status == ArgoAppNotSynced {
		// display sync status and last sync message
		// ...
		argo.handleDeploymentTimeout(task, errors.New(""))
	}

	// application is not in a healthy status
	if status == ArgoAppNotHealthy {
		// display current health of pods
		// ...
		argo.handleDeploymentTimeout(task, errors.New(""))
	}

	// application synced successfully
	if status == ArgoAppSuccess {
		argo.handleDeploymentSuccess(task)
	}
}

func (argo *Argo) handleAppNotFound(task m.Task, err error) {
	rlog.Errorf("[%s] Application %s does not exist.", task.Id, task.App)
	reason := fmt.Sprintf("Argo CD API Error\n\r%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusAppNotFoundMessage, reason)
}

func (argo *Argo) handleArgoUnavailable(task m.Task, err error) {
	rlog.Errorf("[%s] ArgoCD is not available. Aborting.", task.Id)
	reason := fmt.Sprintf("Argo CD API Error\n\r%s", err.Error())
	argo.state.SetTaskStatus(task.Id, "aborted", reason)
}

func (argo *Argo) handleDeploymentTimeout(task m.Task, err error) {
	rlog.Errorf("[%s] Deployment timed out. Aborting.", task.Id)
	rlog.Errorf("[%s] Deployment error\n%s", task.Id, err.Error())
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Deployment timeout\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleAppNotAvailable(task m.Task, err error) {
	rlog.Errorf("[%s] Deployment failed. Application not available\n%s", task.Id, err.Error())
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application not available\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleDeploymentFailed(task m.Task, err error) {
	rlog.Errorf("[%s] Deployment failed. Aborting with error: %s", task.Id, err)
	failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Argo CD API Error\n\r%s", err.Error())
	argo.state.SetTaskStatus(task.Id, config.StatusFailedMessage, reason)
}

func (argo *Argo) handleDeploymentSuccess(task m.Task) {
	rlog.Infof("[%s] App is running on the excepted version.", task.Id)
	failedDeployment.With(prometheus.Labels{"app": task.App}).Set(0)
	argo.state.SetTaskStatus(task.Id, "deployed", "")
}

func (argo *Argo) GetAppList() []string {
	return argo.state.GetAppList()
}

func (argo *Argo) SimpleHealthCheck() bool {
	return argo.state.Check()
}
