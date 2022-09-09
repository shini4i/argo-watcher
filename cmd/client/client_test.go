package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	m "github.com/shini4i/argo-watcher/internal/models"
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

func init() {
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
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
		err := json.NewEncoder(w).Encode(m.TaskStatus{
			Status: "accepted",
			Id:     taskId,
		})
		if err != nil {
			panic(err)
		}
	})
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
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
			status = statusDeployed
		case appNotFoundId:
			status = statusNotFound
		case argocdUnavailableId:
			status = statusArgoCDUnavailable
		case failedTaskId:
			status = statusFailed
		}

		err := json.NewEncoder(w).Encode(m.TaskStatus{
			Status: status,
			Id:     taskId,
		})
		if err != nil {
			panic(err)
		}
	})
	server = httptest.NewServer(mux)
	client = &Watcher{baseUrl: server.URL, client: server.Client()}
}

func TestAddTask(t *testing.T) {
	expected := m.TaskStatus{
		Status: "accepted",
		Id:     taskId,
	}

	task := m.Task{
		App:     "test",
		Author:  "John Doe",
		Project: "Example",
		Images: []m.Image{
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

	status := client.getTaskStatus(taskId)
	if status != "deployed" {
		t.Errorf(messageTemplate, "deployed", status)
	}

	status = client.getTaskStatus(appNotFoundId)
	if status != "app not found" {
		t.Errorf(messageTemplate, "app not found", status)
	}

	status = client.getTaskStatus(argocdUnavailableId)
	if status != "ArgoCD is unavailable" {
		t.Errorf(messageTemplate, "ArgoCD is unavailable", status)
	}

	status = client.getTaskStatus(failedTaskId)
	if status != "failed" {
		t.Errorf(messageTemplate, "failed", status)
	}
}

func TestGetImagesList(t *testing.T) {

	tag = testVersion

	expectedList := []m.Image{
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
