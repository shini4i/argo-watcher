package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

// newTestWatcher builds a Watcher pointing at the given URL with retries enabled
// but a zero backoff, so retry-path tests run instantly.
func newTestWatcher(url string) *Watcher {
	watcher := NewWatcher(url, false, 30*time.Second)
	watcher.retryDelay = 0
	return watcher
}

// flakyTransport fails the first `failures` round-trips with a network error,
// then delegates to fallback. It counts every attempt so tests can assert how
// many requests actually left the client.
type flakyTransport struct {
	failures int32
	calls    int32
	fallback http.RoundTripper
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := atomic.AddInt32(&f.calls, 1)
	if n <= f.failures {
		return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	}
	return f.fallback.RoundTrip(req)
}

// TestGetJSON_RetriesOn5xxThenSucceeds verifies a transient 5xx is retried and
// the eventual 200 is decoded, rather than aborting the whole process.
func TestGetJSON_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if calls.Add(1) <= 2 {
			rw.WriteHeader(http.StatusServiceUnavailable)
			_, _ = rw.Write([]byte(`{"error":"temporarily unavailable"}`))
			return
		}
		_, _ = rw.Write([]byte(`{"message":"OK"}`))
	}))
	defer server.Close()

	watcher := newTestWatcher(server.URL)
	var resp struct {
		Message string `json:"message"`
	}
	err := watcher.getJSON(server.URL, &resp)

	assert.NoError(t, err)
	assert.Equal(t, "OK", resp.Message)
	assert.Equal(t, int32(3), calls.Load(), "two 503s then a 200 should be three requests")
}

// TestGetJSON_RetriesNetworkErrorThenSucceeds verifies a transport-level failure
// (connection refused/reset, DNS, timeout) is retried.
func TestGetJSON_RetriesNetworkErrorThenSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, _ = rw.Write([]byte(`{"message":"OK"}`))
	}))
	defer server.Close()

	transport := &flakyTransport{failures: 2, fallback: server.Client().Transport}
	watcher := newTestWatcher(server.URL)
	watcher.client.Transport = transport

	var resp struct {
		Message string `json:"message"`
	}
	err := watcher.getJSON(server.URL, &resp)

	assert.NoError(t, err)
	assert.Equal(t, "OK", resp.Message)
	assert.Equal(t, int32(3), atomic.LoadInt32(&transport.calls), "two dial failures then success should be three attempts")
}

// TestGetJSON_ExhaustsRetriesOnPersistent5xx verifies retries are bounded and
// the final error surfaces the server status.
func TestGetJSON_ExhaustsRetriesOnPersistent5xx(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte(`{"error":"bad gateway"}`))
	}))
	defer server.Close()

	watcher := newTestWatcher(server.URL)
	var dummy struct{}
	err := watcher.getJSON(server.URL, &dummy)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "502")
	assert.Equal(t, int32(maxTransientRetries+1), calls.Load(), "should try once then retry maxTransientRetries times")
}

// TestGetJSON_DoesNotRetryMalformedBody verifies a 200 response with an
// unparseable body is terminal: retrying an unchanging bad payload never
// succeeds, so it must fail on the first attempt.
func TestGetJSON_DoesNotRetryMalformedBody(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		_, _ = rw.Write([]byte(`{`))
	}))
	defer server.Close()

	watcher := newTestWatcher(server.URL)
	var dummy struct{}
	err := watcher.getJSON(server.URL, &dummy)

	assert.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "a malformed 200 body must not be retried")
}

// TestGetJSON_ExhaustsRetriesOnNetworkError verifies a persistent transport-level
// failure is retried up to the bound and then surfaces the underlying error.
func TestGetJSON_ExhaustsRetriesOnNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, _ = rw.Write([]byte(`{"message":"OK"}`))
	}))
	defer server.Close()

	// Fail every attempt so the retry budget is exhausted.
	transport := &flakyTransport{failures: maxTransientRetries + 1, fallback: server.Client().Transport}
	watcher := newTestWatcher(server.URL)
	watcher.client.Transport = transport

	var dummy struct{}
	err := watcher.getJSON(server.URL, &dummy)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assert.Equal(t, int32(maxTransientRetries+1), atomic.LoadInt32(&transport.calls), "should try once then retry maxTransientRetries times")
}

// TestGetJSON_DoesNotRetryTerminalError verifies a 4xx (auth failure) fails fast
// without wasting retries — retrying a rejected token never succeeds.
func TestGetJSON_DoesNotRetryTerminalError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		rw.WriteHeader(http.StatusUnauthorized)
		_, _ = rw.Write([]byte(`{"error":"deploy token is invalid"}`))
	}))
	defer server.Close()

	watcher := newTestWatcher(server.URL)
	var dummy struct{}
	err := watcher.getJSON(server.URL, &dummy)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Equal(t, int32(1), calls.Load(), "a terminal 4xx must not be retried")
}

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

// TestGetJSON_NonOKResponseSurfacesBody verifies that when the server returns
// a non-200 status, the client error includes whatever the server sent in the
// response body — not just the status code. This is the difference between
// "received non-200 status code: 401" (useless) and "received status 401:
// deploy token is invalid" (actionable).
func TestGetJSON_NonOKResponseSurfacesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusUnauthorized)
		_, _ = rw.Write([]byte(`{"error":"deploy token is invalid"}`))
	}))
	defer server.Close()

	watcher := NewWatcher(server.URL, false, 30*time.Second)
	var dummy struct{}
	err := watcher.getJSON(server.URL, &dummy)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "deploy token is invalid")
}

// TestServerErrorFromResponse exercises the branches of serverErrorFromResponse
// directly: the empty-body fallback (status code only), the 401/403 auth hint,
// and the raw-body fallback when the body is not TaskStatus JSON.
func TestServerErrorFromResponse(t *testing.T) {
	const authHint = "check ARGO_WATCHER_DEPLOY_TOKEN or BEARER_TOKEN"

	t.Run("empty body falls back to status code only", func(t *testing.T) {
		err := serverErrorFromResponse(http.StatusBadGateway, []byte("   "))

		assert.Error(t, err)
		assert.Equal(t, "argo-watcher returned status 502", err.Error())
		assert.NotContains(t, err.Error(), ": ", "must not emit a trailing colon with an empty reason")
	})

	t.Run("401 appends the auth hint", func(t *testing.T) {
		err := serverErrorFromResponse(http.StatusUnauthorized, []byte(`{"error":"deploy token is invalid"}`))

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "401")
		assert.Contains(t, err.Error(), "deploy token is invalid")
		assert.Contains(t, err.Error(), authHint)
	})

	t.Run("403 appends the auth hint", func(t *testing.T) {
		err := serverErrorFromResponse(http.StatusForbidden, []byte(`{"error":"not a member of any privileged group"}`))

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "403")
		assert.Contains(t, err.Error(), "not a member of any privileged group")
		assert.Contains(t, err.Error(), authHint)
	})

	t.Run("non-auth status surfaces reason without the auth hint", func(t *testing.T) {
		err := serverErrorFromResponse(http.StatusServiceUnavailable, []byte("gateway timeout"))

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "503")
		assert.Contains(t, err.Error(), "gateway timeout")
		assert.NotContains(t, err.Error(), authHint)
	})
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
		config := &Config{
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
		config := &Config{
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
	clientConfig = &Config{
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
		"Neither deploy token nor JSON Web token found, git commit will not be performed\n"

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
		// Create a new Watcher instance with an invalid URL. A dial failure is
		// transient, so use the zero-backoff test watcher to skip retry sleeps.
		invalidURL := "http://invalid-url"
		watcher := newTestWatcher(invalidURL)

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
	config := &Config{
		Url:   "http://localhost:8080",
		Debug: true,
	}

	// Call the function
	watcher := setupWatcher(config)

	// Assert the watcher's properties
	assert.Equal(t, config.Url, watcher.baseUrl)
	assert.Equal(t, config.Debug, watcher.debugMode)
}
