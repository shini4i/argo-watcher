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
	cancelledTaskId     = "be8c42c0-a645-11ec-8ea5-f2c4bb72758e"
	unhandledStatusId   = "be8c42c0-a645-11ec-8ea5-f2c4bb72758f"
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
	case cancelledTaskId:
		status = models.StatusCancelledMessage
	case unhandledStatusId:
		status = models.StatusAborted
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

	_, err := watcher.addTask(task, "", "")
	assert.Error(t, err)
}

// TestAddTask_AuthFailureSurfacesServerReason verifies that when the server
// rejects the task (e.g. 401 with `{"error":"deploy token is invalid"}`),
// the client surfaces the server's reason and a hint about which env vars
// govern auth, instead of the old opaque "response code 401".
func TestAddTask_AuthFailureSurfacesServerReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(rw).Encode(models.TaskStatus{
			Status: "unauthorized",
			Error:  "deploy token is invalid",
		})
	}))
	defer server.Close()

	watcher := NewWatcher(server.URL, false, 30*time.Second)
	task := models.Task{
		App: "test", Author: "x", Project: "y",
		Images: []models.Image{{Tag: testVersion, Image: "example"}},
	}

	_, err := watcher.addTask(task, "DeployToken", "wrong")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "deploy token is invalid")
	// The client should hint at which env vars to check on auth failures.
	assert.Contains(t, err.Error(), "ARGO_WATCHER_DEPLOY_TOKEN")
}

// TestAddTask_NonAuthFailureSurfacesServerReason verifies the same body-
// surfacing behaviour for non-auth errors (e.g. 503 with a reason).
func TestAddTask_NonAuthFailureSurfacesServerReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(rw).Encode(models.TaskStatus{
			Status: "down",
			Error:  "argocd is unreachable",
		})
	}))
	defer server.Close()

	watcher := NewWatcher(server.URL, false, 30*time.Second)
	task := models.Task{
		App: "test", Author: "x", Project: "y",
		Images: []models.Image{{Tag: testVersion, Image: "example"}},
	}

	_, err := watcher.addTask(task, "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Contains(t, err.Error(), "argocd is unreachable")
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
	// The retry backoff must default to a non-zero value, otherwise a persistent
	// outage would spin through all retries with no pause (see issue #217).
	assert.Equal(t, defaultRetryDelay, watcher.retryDelay)
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

	taskId, err := client.addTask(task, "", "")
	assert.NoError(t, err)
	assert.Equal(t, expected.Id, taskId)
}

// TestAddTaskJWTHeader verifies the Authorization header the client puts on the
// wire for a JWT. A raw JWT (the maskable GitLab CI form) and a legacy
// "Bearer <jwt>" value must both yield a clean, unprefixed Authorization value
// so backward compatibility is preserved while the raw form is maskable. A
// prefix-only value ("Bearer ") is a misconfiguration that collapses to an
// empty header rather than being sent verbatim — pinned here so the behavior
// cannot change undetected.
func TestAddTaskJWTHeader(t *testing.T) {
	const jwtValue = "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjF9.signature"

	cases := []struct {
		name     string
		input    string
		wantAuth string
	}{
		{"raw JWT (maskable)", jwtValue, jwtValue},
		{"Bearer-prefixed (legacy)", "Bearer " + jwtValue, jwtValue},
		{"prefix only (misconfiguration)", "Bearer ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusAccepted, Id: taskId})
			}))
			defer srv.Close()

			watcher := NewWatcher(srv.URL, false, 30*time.Second)
			_, err := watcher.addTask(models.Task{App: "test"}, "JWT", tc.input)

			assert.NoError(t, err)
			assert.Equal(t, tc.wantAuth, gotAuth, "Authorization header must carry the raw JWT without a Bearer prefix")
		})
	}
}

// TestAddTaskDeployTokenHeader guards that the deploy-token path is unaffected
// by the JWT "Bearer " normalization: the ARGO_WATCHER_DEPLOY_TOKEN header must
// carry the token verbatim, even for a value that happens to start with
// "Bearer ", proving no stripping leaks onto this branch.
func TestAddTaskDeployTokenHeader(t *testing.T) {
	cases := map[string]string{
		"plain token":            "s3cr3t-deploy-token",
		"Bearer-looking literal": "Bearer not-a-jwt",
	}

	for name, tokenInput := range cases {
		t.Run(name, func(t *testing.T) {
			var gotToken string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotToken = r.Header.Get("ARGO_WATCHER_DEPLOY_TOKEN")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusAccepted, Id: taskId})
			}))
			defer srv.Close()

			watcher := NewWatcher(srv.URL, false, 30*time.Second)
			_, err := watcher.addTask(models.Task{App: "test"}, "DeployToken", tokenInput)

			assert.NoError(t, err)
			assert.Equal(t, tokenInput, gotToken, "deploy token must be sent verbatim, never prefix-stripped")
		})
	}
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
		{
			name:          "Cancelled deployment",
			taskId:        cancelledTaskId,
			expectedError: "The deployment was cancelled because a newer deployment superseded it.",
		},
		{
			name:          "Unhandled status exits instead of busy-looping",
			taskId:        unhandledStatusId,
			expectedError: "Received unexpected deployment status",
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

func TestIsDeploymentOverTime(t *testing.T) {
	var tests = []struct {
		retryCount       int
		retryInterval    time.Duration
		expectedDuration time.Duration
		expected         bool
	}{
		{10, 5 * time.Second, 1 * time.Minute, false},
		{13, 5 * time.Second, 1 * time.Minute, true},
		{7, 10 * time.Second, 1 * time.Minute, true},
		{7, 15 * time.Second, 1 * time.Minute, true},
		{0, 2 * time.Second, 1 * time.Minute, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := isDeploymentOverTime(tt.retryCount, tt.retryInterval, tt.expectedDuration)
			if result != tt.expected {
				t.Errorf("for %d retries with %s interval, expected %t but got %t", tt.retryCount, tt.retryInterval, tt.expected, result)
			}
		})
	}
}
