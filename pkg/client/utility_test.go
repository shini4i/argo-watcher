package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestDoRequest(t *testing.T) {
	// Test case 1: The server returns a 200 OK status code
	t.Run("200 status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`OK`))
			assert.NoError(t, err)
		}))
		defer server.Close()

		watcher := NewWatcher(server.URL, false, 30*time.Second)
		resp, err := watcher.doRequest(http.MethodGet, server.URL, nil)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		assert.NoError(t, err)
		assert.Equal(t, "OK", string(body))
	})

	// Test case 2: An error occurs while creating the request
	t.Run("invalid URL", func(t *testing.T) {
		watcher := NewWatcher("http://invalid-url", false, 30*time.Second)
		_, err := watcher.doRequest(http.MethodGet, "http://invalid-url", nil)

		assert.Error(t, err)
	})
}

func TestGetJSON(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Test request parameters
		assert.Equal(t, req.URL.String(), "/test")
		// Send response to be tested
		if _, err := rw.Write([]byte(`{"message": "OK"}`)); err != nil {
			t.Error(err)
		}
	}))
	// Close the server when test finishes
	defer server.Close()

	// Create a new Watcher instance
	watcher := NewWatcher(server.URL, false, 30*time.Second)

	// Define a struct to hold the response
	type response struct {
		Message string `json:"message"`
	}
	var resp response

	// Call getJSON method
	err := watcher.getJSON(server.URL+"/test", &resp)

	// Assert there was no error
	assert.NoError(t, err)

	// Assert the response was as expected
	assert.Equal(t, "OK", resp.Message)
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

func TestCreateTask(t *testing.T) {
	t.Run("TimeoutProvided", func(t *testing.T) {
		config := &ClientConfig{
			App:         "test-app",
			Author:      "test-author",
			Project:     "test-project",
			Images:      []string{"image1", "image2"},
			Tag:         "test-tag",
			TaskTimeout: 30,
		}

		expectedTask := models.Task{
			App:     "test-app",
			Author:  "test-author",
			Project: "test-project",
			Images: []models.Image{
				{
					Image: "image1",
					Tag:   "test-tag",
				},
				{
					Image: "image2",
					Tag:   "test-tag",
				},
			},
			Timeout: 30,
		}

		task := createTask(config)

		assert.Equal(t, expectedTask, task)
	})

	t.Run("TimeoutNotProvided", func(t *testing.T) {
		config := &ClientConfig{
			App:     "test-app",
			Author:  "test-author",
			Project: "test-project",
			Images:  []string{"image1", "image2"},
			Tag:     "test-tag",
		}

		expectedTask := models.Task{
			App:     "test-app",
			Author:  "test-author",
			Project: "test-project",
			Images: []models.Image{
				{
					Image: "image1",
					Tag:   "test-tag",
				},
				{
					Image: "image2",
					Tag:   "test-tag",
				},
			},
		}

		task := createTask(config)

		assert.Equal(t, expectedTask, task)
		assert.Zero(t, task.Timeout)
	})
}

func TestPrintClientConfiguration(t *testing.T) {
	// Initialize clientConfig
	clientConfig = &ClientConfig{
		Url:     "http://localhost:8080",
		Images:  []string{"image1", "image2"},
		Tag:     "test-tag",
		App:     "test-app",
		Author:  "test-author",
		Project: "test-project",
		Token:   "",
		Timeout: 30 * time.Second,
		Debug:   true,
	}

	// Create a Watcher and Task for testing
	watcher := NewWatcher("http://localhost:8080", true, 30*time.Second)
	task := models.Task{
		App:     "test-app",
		Author:  "test-author",
		Project: "test-project",
		Images: []models.Image{
			{
				Image: "image1",
				Tag:   "test-tag",
			},
			{
				Image: "image2",
				Tag:   "test-tag",
			},
		},
	}

	// Expected output
	expectedOutput := "Got the following configuration:\n" +
		"ARGO_WATCHER_URL: http://localhost:8080\n" +
		"ARGO_APP: test-app\n" +
		"COMMIT_AUTHOR: test-author\n" +
		"PROJECT_NAME: test-project\n" +
		"IMAGE_TAG: test-tag\n" +
		"IMAGES: [{image1 test-tag} {image2 test-tag}]\n\n" +
		"Neither deploy token nor JSON Web token found, git commit will not be performed\\n"

	// Redirect standard output to a buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Call the function
	printClientConfiguration(watcher, task)

	// Restore standard output
	err := w.Close()
	assert.NoError(t, err)

	os.Stdout = oldStdout

	// Read the buffer
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	// Compare the buffer's content with the expected output
	assert.Equal(t, expectedOutput, buf.String())
}

func TestGenerateAppUrl(t *testing.T) {
	t.Run("SuccessScenarioAlias", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			// Test request parameters
			assert.Equal(t, req.URL.String(), "/api/v1/config")

			// Create and send the response data
			configResponse := struct {
				ArgoCDURL      url.URL `json:"argo_cd_url"`
				ArgoCDURLAlias string  `json:"argo_cd_url_alias"`
			}{
				ArgoCDURL:      url.URL{Scheme: "http", Host: "localhost:8080"},
				ArgoCDURLAlias: "https://argo-cd.example.com",
			}

			jsonData, _ := json.Marshal(configResponse)
			_, err := rw.Write(jsonData)
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		// Create a new Watcher instance
		watcher := NewWatcher(server.URL, false, 30*time.Second)

		// Create a Task for testing
		task := models.Task{
			App: "test-app",
		}

		// Call the function
		appUrl, err := generateAppUrl(watcher, task)

		// Assert no error
		assert.Nil(t, err)

		// Expected output
		expectedOutput := "https://argo-cd.example.com/applications/test-app"

		// Compare the function's output with the expected output
		assert.Equal(t, expectedOutput, appUrl)
	})

	t.Run("SuccessScenarioNoAlias", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			// Test request parameters
			assert.Equal(t, req.URL.String(), "/api/v1/config")

			// Create and send the response data
			configResponse := struct {
				ArgoCDURL url.URL `json:"argo_cd_url"`
			}{
				ArgoCDURL: url.URL{Scheme: "http", Host: "localhost:8080"},
			}

			jsonData, _ := json.Marshal(configResponse)
			_, err := rw.Write(jsonData)
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		// Create a new Watcher instance
		watcher := NewWatcher(server.URL, false, 30*time.Second)

		// Create a Task for testing
		task := models.Task{
			App: "test-app",
			// other task fields...
		}

		// Call the function
		appUrl, err := generateAppUrl(watcher, task)

		// Assert no error
		assert.Nil(t, err)

		// Expected output
		expectedOutput := "http://localhost:8080/applications/test-app"

		// Compare the function's output with the expected output
		assert.Equal(t, expectedOutput, appUrl)
	})

	t.Run("ErrorScenario", func(t *testing.T) {
		// Create a new Watcher instance with an invalid URL
		invalidURL := "http://invalid-url"
		watcher := NewWatcher(invalidURL, false, 30*time.Second)

		// Create a Task for testing
		task := models.Task{
			App: "test-app",
		}

		// Call the function
		appUrl, err := generateAppUrl(watcher, task)

		// Assert that an error is returned
		assert.NotNil(t, err)

		// Assert that the returned URL is empty
		assert.Equal(t, "", appUrl)
	})
}

func TestSetupWatcher(t *testing.T) {
	// Define the input
	config := &ClientConfig{
		Url:   "http://localhost:8080",
		Debug: true,
	}

	// Call the function
	watcher := setupWatcher(config)

	// Assert the watcher's properties
	assert.Equal(t, config.Url, watcher.baseUrl)
	assert.Equal(t, config.Debug, watcher.debugMode)
}
