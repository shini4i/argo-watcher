package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

var (
	mux                 = http.NewServeMux()
	server              *httptest.Server
	client              *Watcher
	testVersion         = "v0.1.0"
	taskId              = "be8c42c0-a645-11ec-8ea5-f2c4bb72758a"
	failedTaskId        = "be8c42c0-a645-11ec-8ea5-f2c4bb72758b"
	appNotFoundId       = "be8c42c0-a645-11ec-8ea5-f2c4bb72758c"
	argocdUnavailableId = "be8c42c0-a645-11ec-8ea5-f2c4bb72758d"
)

func addTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, err := w.Write([]byte(`Method not allowed`))
		if err != nil {
			fmt.Println(err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	err := json.NewEncoder(w).Encode(models.TaskStatus{
		Status: models.StatusAccepted,
		Id:     taskId,
	})
	if err != nil {
		panic(err)
	}
}

func getTaskStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, err := w.Write([]byte(`Method not allowed`))
		if err != nil {
			fmt.Println(err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	var status string

	id := strings.Split(r.URL.Path, "/")[4]

	switch id {
	case taskId:
		status = models.StatusDeployedMessage
	case appNotFoundId:
		status = models.StatusAppNotFoundMessage
	case argocdUnavailableId:
		status = models.StatusArgoCDUnavailableMessage
	case failedTaskId:
		status = models.StatusFailedMessage
	}

	err := json.NewEncoder(w).Encode(models.TaskStatus{
		Status: status,
		Id:     taskId,
	})
	if err != nil {
		panic(err)
	}
}

func init() {
	mux.HandleFunc("/api/v1/tasks", addTaskHandler)
	mux.HandleFunc("/api/v1/tasks/", getTaskStatusHandler)
	server = httptest.NewServer(mux)
	client = &Watcher{baseUrl: server.URL, client: server.Client()}
}

func TestAddTask(t *testing.T) {
	expected := models.TaskStatus{
		Status: models.StatusAccepted,
		Id:     taskId,
	}

	task := models.Task{
		App:     "test",
		Author:  "John Doe",
		Project: "Example",
		Images: []models.Image{
			{
				Tag:   testVersion,
				Image: "example",
			},
		},
	}

	taskId, err := client.addTask(task, "")
	assert.NoError(t, err)
	assert.Equal(t, expected.Id, taskId)
}

func TestGetTaskStatus(t *testing.T) {
	task, err := client.getTaskStatus(taskId)
	assert.NoError(t, err)
	assert.Equal(t, models.StatusDeployedMessage, task.Status)

	task, err = client.getTaskStatus(appNotFoundId)
	assert.NoError(t, err)
	assert.Equal(t, models.StatusAppNotFoundMessage, task.Status)

	task, err = client.getTaskStatus(argocdUnavailableId)
	assert.NoError(t, err)
	assert.Equal(t, models.StatusArgoCDUnavailableMessage, task.Status)

	task, err = client.getTaskStatus(failedTaskId)
	assert.NoError(t, err)
	assert.Equal(t, models.StatusFailedMessage, task.Status)
}

func TestGetImagesList(t *testing.T) {
	expectedList := []models.Image{
		{
			Image: "example/app",
			Tag:   testVersion,
		},
		{
			Image: "example/web",
			Tag:   testVersion,
		},
	}

	images := getImagesList([]string{"example/app", "example/web"}, testVersion)

	assert.Equal(t, expectedList, images)
}
