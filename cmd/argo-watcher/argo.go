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
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"time"

	s "github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
	"strings"
)

const (
	unavailableMessage = "ArgoCD is unavailable"
	appNotFoundMessage = "app not found"
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

	req, err := http.NewRequest("POST", argo.Url+"/api/v1/session", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	skipTlsVerify, _ := strconv.ParseBool(h.GetEnv("SKIP_TLS_VERIFY", "false"))

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTlsVerify},
	}

	argo.client = &http.Client{
		Jar:       jar,
		Transport: transport,
	}

	err = retry.Do(
		func() error {
			resp, err := argo.client.Do(req)
			if err != nil {
				rlog.Errorf("Couldn't establish connection to ArgoCD, got the following error: %s", err)
				return err
			}

			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					panic(err)
				}
			}(resp.Body)

			return nil
		},
		retry.Attempts(0),
		retry.Delay(15*time.Second),
	)
}

func (argo *Argo) Check() (string, error) {
	if argo == nil {
		argocdUnavailable.Set(1)
		return "", errors.New("argo-watcher is not initialized yet")
	}

	req, err := http.NewRequest("GET", argo.Url+"/api/v1/session/userinfo", nil)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := argo.client.Do(req)
	if err != nil {
		rlog.Error(err)
		argocdUnavailable.Set(1)
		return unavailableMessage, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
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
	rlog.Infof("[%s] A new task was triggered. Expecting tag %s in app %s.", task.Id, task.Images[0].Tag, task.App)
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
	} else {
		return m.TasksResponse{
			Tasks: tasks,
		}
	}
}

func (argo *Argo) GetTaskStatus(id string) string {
	return argo.state.GetTaskStatus(id)
}

func (argo *Argo) checkAppStatus(app string) (m.Application, error) {
	req, err := http.NewRequest("GET", argo.Url+"/api/v1/applications/"+app, nil)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	q := req.URL.Query()
	q.Add("refresh", "normal")
	req.URL.RawQuery = q.Encode()

	resp, err := argo.client.Do(req)
	if err != nil {
		rlog.Error(err)
		return m.Application{}, errors.New(unavailableMessage)
	}

	if resp.StatusCode == 404 {
		return m.Application{}, errors.New(appNotFoundMessage)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(resp.Body)

	var argoApp m.Application
	err = json.Unmarshal(body, &argoApp)
	if err != nil {
		panic(err)
	}

	return argoApp, nil
}

func (argo *Argo) waitForRollout(task m.Task) {

	argoTimeout, err := strconv.Atoi(os.Getenv("ARGO_TIMEOUT"))
	// Need to reconsider this approach
	retryAttempts := uint((4 * (argoTimeout / 60)) + 1)

	err = retry.Do(
		func() error {
			app, err := argo.checkAppStatus(task.App)

			if err != nil && h.Contains([]string{appNotFoundMessage, unavailableMessage}, err.Error()) {
				return err
			}

			for _, image := range task.Images {
				expected := fmt.Sprintf("%s:%s", image.Image, image.Tag)
				if !h.Contains(app.Status.Summary.Images, expected) {
					rlog.Debugf("[%s] %s is not available yet", task.Id, expected)
					return errors.New("")
				}
			}

			if app.Status.Sync.Status != "Synced" || app.Status.Health.Status != "Healthy" {
				rlog.Debugf("[%s] %s is not ready yet", task.Id, task.App)
				return errors.New("")
			}

			return nil
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(15*time.Second),
		retry.Attempts(retryAttempts),
		retry.RetryIf(func(err error) bool {
			return !h.Contains([]string{appNotFoundMessage, unavailableMessage}, err.Error())
		}),
	)

	if err == nil {
		rlog.Infof("[%s] App is running on the excepted version.", task.Id)
		failedDeployment.With(prometheus.Labels{"app": task.App}).Set(0)
		argo.state.SetTaskStatus(task.Id, "deployed")
	} else if strings.Contains(err.Error(), unavailableMessage) {
		rlog.Errorf("[%s] ArgoCD is unavailable. Aborting.", task.Id)
		argo.state.SetTaskStatus(task.Id, "aborted")
	} else if strings.Contains(err.Error(), appNotFoundMessage) {
		rlog.Errorf("[%s] Application %s does not exist", task.Id, task.App)
		argo.state.SetTaskStatus(task.Id, appNotFoundMessage)
	} else {
		rlog.Infof("[%s] The expected tag did not become available within expected timeframe. Aborting.", task.Id)
		failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
		argo.state.SetTaskStatus(task.Id, "failed")
	}
}

func (argo *Argo) GetAppList() []string {
	return argo.state.GetAppList()
}

func (argo *Argo) SimpleHealthCheck() bool {
	return argo.state.Check()
}
