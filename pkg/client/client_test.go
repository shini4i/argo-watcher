package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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

	if err := json.NewEncoder(w).Encode(models.TaskStatus{
		Status: status,
		Id:     taskId,
	}); err != nil {
		panic(err)
	}
}

func TestAddTaskServerError(t *testing.T) {
	// Create a test server that always returns a 500 status code
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Create a new Watcher instance
	watcher := NewWatcher(server.URL, false, 30*time.Second)

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

	_, err := watcher.addTask(task, "")
	assert.Error(t, err)
}

func init() {
	mux.HandleFunc("/api/v1/tasks", addTaskHandler)
	mux.HandleFunc("/api/v1/tasks/", getTaskStatusHandler)
	server = httptest.NewServer(mux)
	client = &Watcher{baseUrl: server.URL, client: server.Client()}
}

func TestNewWatcher(t *testing.T) {
	baseUrl := "http://localhost:8080"
	debugMode := true
	timeout := 30 * time.Second

	watcher := NewWatcher(baseUrl, debugMode, timeout)

	assert.Equal(t, baseUrl, watcher.baseUrl)
	assert.Equal(t, debugMode, watcher.debugMode)
	assert.NotNil(t, watcher.client)
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
	t.Run("received deployed status", func(t *testing.T) {
		task, err := client.getTaskStatus(taskId)
		assert.NoError(t, err)
		assert.Equal(t, models.StatusDeployedMessage, task.Status)
	})

	t.Run("received app not found status", func(t *testing.T) {
		task, err := client.getTaskStatus(appNotFoundId)
		assert.NoError(t, err)
		assert.Equal(t, models.StatusAppNotFoundMessage, task.Status)
	})

	t.Run("received argocd unavailable status", func(t *testing.T) {
		task, err := client.getTaskStatus(argocdUnavailableId)
		assert.NoError(t, err)
		assert.Equal(t, models.StatusArgoCDUnavailableMessage, task.Status)
	})

	t.Run("received failed status", func(t *testing.T) {
		task, err := client.getTaskStatus(failedTaskId)
		assert.NoError(t, err)
		assert.Equal(t, models.StatusFailedMessage, task.Status)
	})

	// Test case: server returns invalid JSON
	t.Run("server returns invalid JSON", func(t *testing.T) {
		// Create a test server that always returns an invalid JSON response
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, _ = rw.Write([]byte(`invalid JSON`))
		}))
		defer server.Close()

		// Create a new Watcher instance
		watcher := NewWatcher(server.URL, false, 30*time.Second)

		// Call the function
		_, err := watcher.getTaskStatus("test-id")

		// We expect an error
		assert.Error(t, err)
	})
}

func TestGetWatcherConfig(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Test request parameters
		assert.Equal(t, req.URL.String(), "/api/v1/config")

		// Create the response data
		configResponse := struct {
			ArgoCDURL      url.URL `json:"argo_cd_url"`
			ArgoCDURLAlias string  `json:"argo_cd_url_alias"`
		}{
			ArgoCDURL:      url.URL{Scheme: "http", Host: "localhost:8080"},
			ArgoCDURLAlias: "https://argo-cd.example.com",
		}

		// Marshal the response data to JSON
		jsonData, err := json.Marshal(configResponse)
		if err != nil {
			t.Error(err)
			return
		}

		// Write the JSON data to the response writer
		if _, err := rw.Write(jsonData); err != nil {
			t.Error(err)
		}
	}))
	// Close the server when test finishes
	defer server.Close()

	// Create a new Watcher instance
	watcher := NewWatcher(server.URL, false, 30*time.Second)

	// Call getWatcherConfig method
	serverConfig, err := watcher.getWatcherConfig()

	// Assert there was no error
	assert.NoError(t, err)

	// Assert the response was as expected
	expectedUrl, _ := url.Parse("http://localhost:8080")
	assert.Equal(t, expectedUrl, &serverConfig.ArgoUrl)
	assert.Equal(t, "https://argo-cd.example.com", serverConfig.ArgoUrlAlias)
}

func TestWaitForDeployment(t *testing.T) {
	testCases := []struct {
		name          string
		taskId        string
		expectedError string
	}{
		{
			name:          "Successful deployment",
			taskId:        taskId,
			expectedError: "",
		},
		{
			name:          "Failed deployment",
			taskId:        failedTaskId,
			expectedError: "The deployment has failed, please check logs.",
		},
		{
			name:          "Application not found",
			taskId:        appNotFoundId,
			expectedError: "Application test does not exist.",
		},
		{
			name:          "ArgoCD unavailable",
			taskId:        argocdUnavailableId,
			expectedError: "ArgoCD is unavailable. Please investigate.",
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.waitForDeployment(tc.taskId, "test", testVersion)
			if tc.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
			}
		})
	}
}
