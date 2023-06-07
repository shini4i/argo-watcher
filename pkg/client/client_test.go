package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/shini4i/argo-watcher/internal/models"
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
	if r.Method != "POST" {
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
		Status: "accepted",
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
		Status: "accepted",
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

	id := client.addTask(task)

	if id != expected.Id {
		t.Errorf("Expected id %s, got %s", expected.Id, id)
	}
}

func TestGetTaskStatus(t *testing.T) {
	messageTemplate := "Expected status %s, got %s"

	status := client.getTaskStatus(taskId).Status
	if status != models.StatusDeployedMessage {
		t.Errorf(messageTemplate, "deployed", status)
	}

	status = client.getTaskStatus(appNotFoundId).Status
	if status != models.StatusAppNotFoundMessage {
		t.Errorf(messageTemplate, "app not found", status)
	}

	status = client.getTaskStatus(argocdUnavailableId).Status
	if status != models.StatusArgoCDUnavailableMessage {
		t.Errorf(messageTemplate, "ArgoCD is unavailable", status)
	}

	status = client.getTaskStatus(failedTaskId).Status
	if status != models.StatusFailedMessage {
		t.Errorf(messageTemplate, "failed", status)
	}
}

func TestGetImagesList(t *testing.T) {

	tag = testVersion

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

	err := os.Setenv("IMAGES", "example/app,example/web")
	if err != nil {
		return
	}

	images := getImagesList()

	if !reflect.DeepEqual(images, expectedList) {
		t.Errorf("Expected list %v, got %v", expectedList, images)
	}
}
