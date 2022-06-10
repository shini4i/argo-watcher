package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avast/retry-go"
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
)

type Argo struct {
	Url      string
	User     string
	Password string
	client   *http.Client
	state    s.State
}

func (argo *Argo) Init() *Argo {
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
	}

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

	client := &http.Client{
		Jar:       jar,
		Transport: transport,
	}

	response, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	return &Argo{
		Url:    argo.Url,
		client: client,
		state:  argo.state,
	}
}

func (argo *Argo) Check() string {
	req, err := http.NewRequest("GET", argo.Url+"/api/v1/session/userinfo", nil)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	if err != nil {
		panic(err)
	}

	resp, err := argo.client.Do(req)
	if err != nil {
		panic(err)
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

	type userinfo struct {
		LoggedIn bool   `json:"loggedIn"`
		Username string `json:"username"`
	}

	var userInfo userinfo
	err = json.Unmarshal(body, &userInfo)
	if err != nil {
		panic(err)
	}

	if userInfo.LoggedIn && argo.state.Check() {
		return "up"
	} else {
		return "down"
	}
}

func (argo *Argo) AddTask(task m.Task) string {
	task.Id = uuid.New().String()
	rlog.Infof("[%s] A new task was triggered. Expecting tag %s in app %s.", task.Id, task.Images[0].Tag, task.App)
	argo.state.Add(task)
	processedDeployments.Inc()
	go argo.waitForRollout(task)
	return task.Id
}

func (argo *Argo) GetTasks(startTime float64, endTime float64, app string) []m.Task {
	return argo.state.GetTasks(startTime, endTime, app)
}

func (argo *Argo) GetTaskStatus(id string) string {
	return argo.state.GetTaskStatus(id)
}

func (argo *Argo) checkAppStatus(app string) (m.Application, error) {
	req, err := http.NewRequest("GET", argo.Url+"/api/v1/applications/"+app, nil)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	if err != nil {
		panic(err)
	}

	q := req.URL.Query()
	q.Add("refresh", "normal")
	req.URL.RawQuery = q.Encode()

	resp, err := argo.client.Do(req)
	if err != nil {
		panic(err)
	}

	if resp.StatusCode == 404 {
		return m.Application{}, errors.New("app not found")
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
			if err != nil {
				argo.state.SetTaskStatus(task.Id, "app not found")
				return nil
			}

			for _, image := range task.Images {
				expected := fmt.Sprintf("%s:%s", image.Image, image.Tag)
				for _, currentImage := range app.Status.Summary.Images {
					if expected != currentImage {
						return errors.New("")
					} else {
						rlog.Infof("[%s] App is running on the excepted version.", task.Id)
						failedDeployment.With(prometheus.Labels{"app": task.App}).Set(0)
						argo.state.SetTaskStatus(task.Id, "deployed")
					}
				}
			}
			return nil
		},
		retry.OnRetry(func(n uint, err error) {
			rlog.Debugf("[%s] The expected tag was not found. Retrying...", task.Id)
		}),
		retry.DelayType(retry.FixedDelay),
		retry.Delay(15*time.Second),
		retry.Attempts(retryAttempts),
	)

	if err != nil {
		rlog.Infof("[%s] The expected tag did not become available within expected timeframe. Aborting.", task.Id)
		failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
		argo.state.SetTaskStatus(task.Id, "failed")
	}
}

func (argo *Argo) GetAppList() []string {
	return argo.state.GetAppList()
}
