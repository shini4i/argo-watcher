package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestWatcher builds a Watcher pointing at the given URL with retries enabled
// but a zero backoff, so retry-path tests run instantly.
func newTestWatcher(url string) *Watcher {
	watcher := NewWatcher(url, false, 30*time.Second)
	watcher.retryDelay = 0
	return watcher
}

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

// Stands in for the whole transport-failure class: connection refused/reset, DNS, timeout.
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

func TestGetJSON_ExhaustsRetriesOnNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, _ = rw.Write([]byte(`{"message":"OK"}`))
	}))
	defer server.Close()

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

	t.Run("invalid URL", func(t *testing.T) {
		watcher := NewWatcher("http://invalid-url", false, 30*time.Second)
		_, err := watcher.doRequest(http.MethodGet, "http://invalid-url", nil)

		assert.Error(t, err)
	})
}

func TestGetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, req.URL.String(), "/test")
		if _, err := rw.Write([]byte(`{"message": "OK"}`)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	watcher := NewWatcher(server.URL, false, 30*time.Second)

	type response struct {
		Message string `json:"message"`
	}
	var resp response

	err := watcher.getJSON(server.URL+"/test", &resp)

	assert.NoError(t, err)

	assert.Equal(t, "OK", resp.Message)
}

// The error must carry the server's response body, not just the status code: "received
// status 401: deploy token is invalid" beats "received non-200 status code: 401".
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

	t.Run("RefreshOverride", func(t *testing.T) {
		refresh := false
		config := &Config{
			App:     "test-app",
			Author:  "test-author",
			Project: "test-project",
			Images:  []string{"image1"},
			Tag:     "test-tag",
			Refresh: &refresh,
		}

		task := createTask(config)

		require.NotNil(t, task.Refresh, "an explicit TASK_REFRESH must propagate to the task")
		assert.False(t, *task.Refresh)
	})

	t.Run("RefreshUnset", func(t *testing.T) {
		config := &Config{
			App:     "test-app",
			Author:  "test-author",
			Project: "test-project",
			Images:  []string{"image1"},
			Tag:     "test-tag",
		}

		task := createTask(config)

		assert.Nil(t, task.Refresh, "an omitted TASK_REFRESH must leave the server default in effect")
	})
}

func TestPrintClientConfiguration(t *testing.T) {
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

	expectedOutput := "Got the following configuration:\n" +
		"ARGO_WATCHER_URL: http://localhost:8080\n" +
		"ARGO_APP: test-app\n" +
		"COMMIT_AUTHOR: test-author\n" +
		"PROJECT_NAME: test-project\n" +
		"IMAGE_TAG: test-tag\n" +
		"IMAGES: [{image1 test-tag} {image2 test-tag}]\n\n" +
		"Neither deploy token nor JSON Web token found, git commit will not be performed\n"

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printClientConfiguration(watcher, task)

	err := w.Close()
	assert.NoError(t, err)

	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	assert.Equal(t, expectedOutput, buf.String())
}

func TestGenerateAppUrl(t *testing.T) {
	t.Run("SuccessScenarioAlias", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/api/v1/config")

			configResponse := struct {
				ArgoCDURL      string `json:"argo_cd_url"`
				ArgoCDURLAlias string `json:"argo_cd_url_alias"`
			}{
				ArgoCDURL:      "http://localhost:8080",
				ArgoCDURLAlias: "https://argo-cd.example.com",
			}

			jsonData, _ := json.Marshal(configResponse)
			_, err := rw.Write(jsonData)
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		watcher := NewWatcher(server.URL, false, 30*time.Second)

		task := models.Task{
			App: "test-app",
		}

		appUrl, err := generateAppUrl(watcher, task)

		assert.Nil(t, err)

		expectedOutput := "https://argo-cd.example.com/applications/test-app"

		assert.Equal(t, expectedOutput, appUrl)
	})

	t.Run("SuccessScenarioNoAlias", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/api/v1/config")

			configResponse := struct {
				ArgoCDURL string `json:"argo_cd_url"`
			}{
				ArgoCDURL: "http://localhost:8080",
			}

			jsonData, _ := json.Marshal(configResponse)
			_, err := rw.Write(jsonData)
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		watcher := NewWatcher(server.URL, false, 30*time.Second)

		task := models.Task{
			App: "test-app",
		}

		appUrl, err := generateAppUrl(watcher, task)

		assert.Nil(t, err)

		expectedOutput := "http://localhost:8080/applications/test-app"

		assert.Equal(t, expectedOutput, appUrl)
	})

	// The alias wins over argo_cd_url, and a trailing slash on it must not produce
	// a doubled one in the link.
	t.Run("SuccessScenarioAliasWithTrailingSlash", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/api/v1/config")

			configResponse := struct {
				ArgoCDURL      string `json:"argo_cd_url"`
				ArgoCDURLAlias string `json:"argo_cd_url_alias"`
			}{
				ArgoCDURL:      "http://argo-cd.internal:8080",
				ArgoCDURLAlias: "https://argo-cd.example.com/",
			}

			jsonData, _ := json.Marshal(configResponse)
			_, err := rw.Write(jsonData)
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		watcher := NewWatcher(server.URL, false, 30*time.Second)

		appUrl, err := generateAppUrl(watcher, models.Task{App: "test-app"})

		assert.Nil(t, err)
		assert.Equal(t, "https://argo-cd.example.com/applications/test-app", appUrl)
	})

	// An ArgoCD published under a sub-path is reachable only with that path kept,
	// and the Web UI has always kept it.
	t.Run("SuccessScenarioUrlWithPath", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/api/v1/config")

			configResponse := struct {
				ArgoCDURL string `json:"argo_cd_url"`
			}{
				ArgoCDURL: "https://platform.example.com/argocd/",
			}

			jsonData, _ := json.Marshal(configResponse)
			_, err := rw.Write(jsonData)
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		watcher := NewWatcher(server.URL, false, 30*time.Second)

		appUrl, err := generateAppUrl(watcher, models.Task{App: "test-app"})

		assert.Nil(t, err)
		assert.Equal(t, "https://platform.example.com/argocd/applications/test-app", appUrl)
	})

	t.Run("ErrorScenario", func(t *testing.T) {
		// Create a new Watcher instance with an invalid URL. A dial failure is
		// transient, so use the zero-backoff test watcher to skip retry sleeps.
		invalidURL := "http://invalid-url"
		watcher := newTestWatcher(invalidURL)

		task := models.Task{
			App: "test-app",
		}

		appUrl, err := generateAppUrl(watcher, task)

		assert.NotNil(t, err)

		assert.Equal(t, "", appUrl)
	})
}

func TestSetupWatcher(t *testing.T) {
	config := &Config{
		Url:   "http://localhost:8080",
		Debug: true,
	}

	watcher := setupWatcher(config)

	assert.Equal(t, config.Url, watcher.baseUrl)
	assert.Equal(t, config.Debug, watcher.debugMode)
	assert.Equal(t, credential{}, watcher.auth, "a client with no token configured carries no credential")

	t.Run("carries the configured deploy token", func(t *testing.T) {
		watcher := setupWatcher(&Config{Url: "http://localhost:8080", Token: "s3cr3t-deploy-token"})

		assert.Equal(t, credential{header: deployTokenHeader, value: "s3cr3t-deploy-token"}, watcher.auth)
	})

	t.Run("carries the configured JWT", func(t *testing.T) {
		watcher := setupWatcher(&Config{Url: "http://localhost:8080", JsonWebToken: testJWT})

		assert.Equal(t, credential{header: jwtHeader, value: testJWT}, watcher.auth)
	})
}
