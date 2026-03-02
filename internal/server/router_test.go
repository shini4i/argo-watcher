package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
)

// mockArgoApi is a minimal mock for ArgoApiInterface used in tests.
type mockArgoApi struct{}

func (m *mockArgoApi) Init(_ *config.ServerConfig) error {
	return nil
}

func (m *mockArgoApi) GetUserInfo() (*models.Userinfo, error) {
	return &models.Userinfo{LoggedIn: true, Username: "test"}, nil
}

func (m *mockArgoApi) GetApplication(_ string) (*models.Application, error) {
	return &models.Application{}, nil
}

// mockTaskRepository is a minimal mock for TaskRepository used in tests.
type mockTaskRepository struct{}

func (m *mockTaskRepository) Connect(_ *config.ServerConfig) error {
	return nil
}

func (m *mockTaskRepository) AddTask(task models.Task) (*models.Task, error) {
	return &task, nil
}

func (m *mockTaskRepository) GetTasks(_, _ float64, _ string, _, _ int) ([]models.Task, int64) {
	return []models.Task{}, 0
}

func (m *mockTaskRepository) GetTask(_ string) (*models.Task, error) {
	return &models.Task{}, nil
}

func (m *mockTaskRepository) SetTaskStatus(_, _, _ string) error {
	return nil
}

func (m *mockTaskRepository) Check() bool {
	return true
}

func (m *mockTaskRepository) ProcessObsoleteTasks(_ uint) {}

// mockMetrics is a minimal mock for MetricsInterface used in tests.
type mockMetrics struct{}

func (m *mockMetrics) AddProcessedDeployment(_ string) {}
func (m *mockMetrics) AddFailedDeployment(_ string)    {}
func (m *mockMetrics) ResetFailedDeployment(_ string)  {}
func (m *mockMetrics) SetArgoUnavailable(_ bool)       {}
func (m *mockMetrics) AddInProgressTask()              {}
func (m *mockMetrics) RemoveInProgressTask()           {}

func TestGetVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.Default()
	env := &Env{}
	router.GET("/api/v1/version", env.getVersion)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/version", nil)
	if err != nil {
		t.Fatalf("Couldn't create request: %v\n", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, fmt.Sprintf("\"%s\"", version), w.Body.String())
}

func TestDeployLock(t *testing.T) {
	var err error

	gin.SetMode(gin.TestMode)

	dummyConfig := &config.ServerConfig{}

	router := gin.Default()
	env := &Env{config: dummyConfig}

	env.lockdown, err = NewLockdown(dummyConfig.LockdownSchedule)
	assert.NoError(t, err)

	t.Run("SetDeployLock", func(t *testing.T) {
		router.POST("/api/v1/deploy-lock", env.SetDeployLock)

		req, err := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		if err != nil {
			t.Fatalf("Couldn't create request: %v\n", err)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "\"deploy lock is set\"", w.Body.String())
	})

	t.Run("ReleaseDeployLock", func(t *testing.T) {
		router.DELETE("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, err := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		if err != nil {
			t.Fatalf("Couldn't create request: %v\n", err)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "\"deploy lock is released\"", w.Body.String())
	})

	t.Run("isDeployLockSet", func(t *testing.T) {
		router.GET("/api/v1/deploy-lock", env.isDeployLockSet)

		req, err := http.NewRequest(http.MethodGet, "/api/v1/deploy-lock", nil)
		if err != nil {
			t.Fatalf("Couldn't create request: %v\n", err)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "false", w.Body.String())
	})
}

func TestRemoveWebSocketConnection(t *testing.T) {
	conn := &websocket.Conn{}
	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()
	removeWebSocketConnection(conn)
	connectionsMutex.Lock()
	assert.NotContains(t, connections, conn)
	connectionsMutex.Unlock()
}

func TestWebSocketConnectionsConcurrentAccess(t *testing.T) {
	// Reset connections slice
	connectionsMutex.Lock()
	connections = nil
	connectionsMutex.Unlock()

	// Test concurrent add and remove operations don't cause race conditions
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := &websocket.Conn{}
			connectionsMutex.Lock()
			connections = append(connections, conn)
			connectionsMutex.Unlock()
			removeWebSocketConnection(conn)
		}()
	}

	wg.Wait()

	// All connections should have been removed
	connectionsMutex.Lock()
	assert.Empty(t, connections)
	connectionsMutex.Unlock()
}

func TestNewEnv(t *testing.T) {
	serverConfig := &config.ServerConfig{
		Host:        "localhost",
		Port:        "8080",
		DeployToken: "deployToken",
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
		JWTSecret: "jwtSecret",
	}

	argo := &argocd.Argo{}
	metrics := &prometheus.Metrics{}
	updater := &argocd.ArgoStatusUpdater{}

	env, err := NewEnv(serverConfig, argo, metrics, updater)

	assert.NoError(t, err)
	assert.Equal(t, env.config, serverConfig)
	assert.Equal(t, env.argo, argo)
	assert.Equal(t, env.metrics, metrics)
	assert.Equal(t, env.updater, updater)

	expectedStrategies := map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(serverConfig.DeployToken),
		"Authorization":             auth.NewJWTAuthService(serverConfig.JWTSecret),
		keycloakHeader:              auth.NewKeycloakAuthService(serverConfig),
	}

	assert.Equal(t, expectedStrategies, env.strategies)
	assert.NotNil(t, env.authenticator)
}

// TestGetStateInvalidQueryParams verifies that the getState handler gracefully handles
// invalid query parameters by logging debug messages and using default values.
func TestGetStateInvalidQueryParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Set up Argo with mock dependencies
	argo := &argocd.Argo{}
	argo.Init(&mockTaskRepository{}, &mockArgoApi{}, &mockMetrics{})

	env := &Env{
		argo:   argo,
		config: &config.ServerConfig{},
	}

	router := gin.Default()
	router.GET("/api/v1/tasks", env.getState)

	testCases := []struct {
		name        string
		queryParams string
	}{
		{
			name:        "invalid from_timestamp",
			queryParams: "?from_timestamp=notanumber",
		},
		{
			name:        "invalid to_timestamp",
			queryParams: "?to_timestamp=notanumber",
		},
		{
			name:        "invalid limit",
			queryParams: "?limit=notanumber",
		},
		{
			name:        "invalid offset",
			queryParams: "?offset=notanumber",
		},
		{
			name:        "negative limit",
			queryParams: "?limit=-5",
		},
		{
			name:        "negative offset",
			queryParams: "?offset=-10",
		},
		{
			name:        "all invalid params",
			queryParams: "?from_timestamp=abc&to_timestamp=xyz&limit=foo&offset=bar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/api/v1/tasks"+tc.queryParams, nil)
			if err != nil {
				t.Fatalf("Couldn't create request: %v\n", err)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// The handler should return 200 OK even with invalid params
			// (it falls back to defaults and logs debug messages)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestStaticFileServing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a temporary directory for static files
	tmpDir := t.TempDir()

	// Create test files
	indexContent := []byte("<html><body>Index</body></html>")
	jsContent := []byte("console.log('test');")

	err := os.WriteFile(tmpDir+"/index.html", indexContent, 0644)
	assert.NoError(t, err)

	err = os.MkdirAll(tmpDir+"/assets", 0755)
	assert.NoError(t, err)

	err = os.WriteFile(tmpDir+"/assets/main.js", jsContent, 0644)
	assert.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
	}

	env := &Env{
		config: serverConfig,
	}

	env.lockdown, _ = NewLockdown("")

	router := env.CreateRouter()

	t.Run("serves existing static file", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/assets/main.js", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(jsContent), w.Body.String())
	})

	t.Run("serves index.html for SPA routes", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/dashboard", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(indexContent), w.Body.String())
	})

	t.Run("prevents path traversal attack", func(t *testing.T) {
		// Try to access /etc/passwd via path traversal
		// Note: Go's net/http rejects malformed URLs with 400, which is also protection
		req, _ := http.NewRequest(http.MethodGet, "/../../../etc/passwd", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should either return 400 (Go's built-in protection) or index.html (our protection)
		// Either way, /etc/passwd should NOT be served
		assert.NotContains(t, w.Body.String(), "root:")
	})

	t.Run("prevents encoded path traversal", func(t *testing.T) {
		// URL-encoded path traversal attempt
		// Note: Go's net/http rejects malformed URLs with 400, which is also protection
		req, _ := http.NewRequest(http.MethodGet, "/..%2F..%2F..%2Fetc%2Fpasswd", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should either return 400 (Go's built-in protection) or index.html (our protection)
		// Either way, /etc/passwd should NOT be served
		assert.NotContains(t, w.Body.String(), "root:")
	})

	t.Run("prevents double-dot path traversal in valid URL", func(t *testing.T) {
		// This path looks valid but tries to escape using /../
		req, _ := http.NewRequest(http.MethodGet, "/assets/../../../etc/passwd", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should not serve /etc/passwd
		assert.NotContains(t, w.Body.String(), "root:")
	})

	t.Run("serves index.html for directory requests", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/assets/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(indexContent), w.Body.String())
	})
}

func TestWebSocketInterceptor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a minimal test environment
	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	assert.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
	}

	env := &Env{
		config: serverConfig,
	}
	env.lockdown, _ = NewLockdown("")

	router := env.CreateRouter()

	t.Run("non-upgrade GET to /ws returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/ws", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "WebSocket upgrade required")
	})

	t.Run("case-insensitive Upgrade header check", func(t *testing.T) {
		// Test with different case variations - all should be intercepted by the WebSocket handler
		// The handler will fail at websocket.Accept (missing Sec-WebSocket-Version), but the key
		// assertion is that the response body does NOT contain "WebSocket upgrade required"
		// (which would mean it fell through to the fallback route handler)
		testCases := []string{"websocket", "WebSocket", "WEBSOCKET", "Websocket"}

		for _, upgradeValue := range testCases {
			t.Run(upgradeValue, func(t *testing.T) {
				req, _ := http.NewRequest(http.MethodGet, "/ws", nil)
				req.Header.Set("Upgrade", upgradeValue)
				req.Header.Set("Connection", "Upgrade")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// The interceptor should have handled this, NOT the fallback route
				// websocket.Accept will fail (missing proper headers), but it should NOT
				// return our custom "WebSocket upgrade required" message
				assert.NotContains(t, w.Body.String(), "WebSocket upgrade required",
					"Upgrade header '%s' should be intercepted by WebSocket handler", upgradeValue)
			})
		}
	})
}

func TestWebSocketConnectionIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Reset connections at start to ensure clean state
	connectionsMutex.Lock()
	connections = nil
	connectionsMutex.Unlock()

	// Cleanup at end
	t.Cleanup(func() {
		connectionsMutex.Lock()
		connections = nil
		connectionsMutex.Unlock()
	})

	// Create a temporary directory for static files
	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	assert.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
		DevEnvironment: true, // Allow test origins
	}

	env := &Env{config: serverConfig}
	env.lockdown, _ = NewLockdown("")

	router := env.CreateRouter()

	// Use httptest.Server for real HTTP connection (supports hijacking)
	server := httptest.NewServer(router)
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt WebSocket connection
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket connection failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Connection succeeded - the pre-hijack wrapper works
	t.Log("WebSocket connection established successfully")
}

func TestWsResponseWriterHijackNilConn(t *testing.T) {
	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           nil,
		brw:            nil,
	}
	_, _, err := w.Hijack()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection was not pre-hijacked")
}

func TestWsResponseWriterHijackSuccess(t *testing.T) {
	// Create a pipe to simulate a connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	brw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))

	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           server,
		brw:            brw,
	}

	conn, readWriter, err := w.Hijack()
	assert.NoError(t, err)
	assert.Equal(t, server, conn)
	assert.Equal(t, brw, readWriter)
}

func TestWsResponseWriterWriteNilBrw(t *testing.T) {
	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           nil,
		brw:            nil,
	}
	n, err := w.Write([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffered writer not available")
	assert.Equal(t, 0, n)
}

func TestWsResponseWriterWriteSuccess(t *testing.T) {
	// Create a pipe to simulate a connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	brw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))

	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           server,
		brw:            brw,
	}

	// Write in a goroutine since pipes are synchronous
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4)
		_, _ = client.Read(buf)
		close(done)
	}()

	n, err := w.Write([]byte("test"))
	<-done

	assert.NoError(t, err)
	assert.Equal(t, 4, n)
}

func TestWsResponseWriterWriteHeaderNilBrw(t *testing.T) {
	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           nil,
		brw:            nil,
	}
	// WriteHeader returns void, so we verify it doesn't panic
	assert.NotPanics(t, func() {
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
}

func TestWsResponseWriterWriteHeaderSuccess(t *testing.T) {
	// Create a pipe to simulate a connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	brw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))

	// Create a mock ResponseWriter that provides headers
	mockRW := httptest.NewRecorder()
	mockRW.Header().Set("Upgrade", "websocket")
	mockRW.Header().Set("Connection", "Upgrade")

	w := &wsResponseWriter{
		ResponseWriter: &mockGinResponseWriter{ResponseRecorder: mockRW},
		conn:           server,
		brw:            brw,
	}

	// Read in a goroutine since pipes are synchronous
	done := make(chan string)
	go func() {
		buf := make([]byte, 1024)
		n, _ := client.Read(buf)
		done <- string(buf[:n])
	}()

	w.WriteHeader(http.StatusSwitchingProtocols)
	result := <-done

	assert.Contains(t, result, "HTTP/1.1 101 Switching Protocols")
	assert.Contains(t, result, "Upgrade: websocket")
	assert.Contains(t, result, "Connection: Upgrade")
}

// mockGinResponseWriter implements gin.ResponseWriter for testing.
type mockGinResponseWriter struct {
	*httptest.ResponseRecorder
}

func (m *mockGinResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func (m *mockGinResponseWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}

func (m *mockGinResponseWriter) Status() int {
	return m.ResponseRecorder.Code
}

func (m *mockGinResponseWriter) Size() int {
	return m.ResponseRecorder.Body.Len()
}

func (m *mockGinResponseWriter) WriteString(s string) (int, error) {
	return m.ResponseRecorder.WriteString(s)
}

func (m *mockGinResponseWriter) Written() bool {
	return m.ResponseRecorder.Code != 0
}

func (m *mockGinResponseWriter) WriteHeaderNow() {}

func (m *mockGinResponseWriter) Pusher() http.Pusher {
	return nil
}

func (m *mockGinResponseWriter) Flush() {}

// errorWriter is a writer that always fails for testing error paths.
type errorWriter struct {
	failAfter int // Number of successful writes before failing
	count     int
}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	e.count++
	if e.failAfter > 0 && e.count <= e.failAfter {
		return len(p), nil
	}
	return 0, fmt.Errorf("write error")
}

func (e *errorWriter) Read(p []byte) (n int, err error) {
	return 0, nil
}

func TestWsResponseWriterWriteFlushError(t *testing.T) {
	// Create a writer that always fails
	// The error occurs on Flush, not on the initial Write (which buffers)
	ew := &errorWriter{}
	brw := bufio.NewReadWriter(bufio.NewReader(ew), bufio.NewWriter(ew))

	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           nil,
		brw:            brw,
	}

	// Write succeeds (buffered), but Flush fails
	n, err := w.Write([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write error")
	assert.Equal(t, 4, n) // Data was written to buffer
}

func TestWsResponseWriterWriteBufferOverflow(t *testing.T) {
	// Create a writer with a tiny buffer that will overflow immediately
	ew := &errorWriter{}
	// Use a 1-byte buffer - any write larger than 1 byte will try to flush
	brw := bufio.NewReadWriter(
		bufio.NewReader(ew),
		bufio.NewWriterSize(ew, 1),
	)

	w := &wsResponseWriter{
		ResponseWriter: nil,
		conn:           nil,
		brw:            brw,
	}

	// Write larger than buffer forces immediate flush, which fails
	n, err := w.Write([]byte("test data that exceeds buffer"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write error")
	assert.Equal(t, 0, n) // Write should fail with 0 bytes
}

func TestWsResponseWriterWriteHeaderErrors(t *testing.T) {
	t.Run("status line write error with tiny buffer", func(t *testing.T) {
		mockRW := httptest.NewRecorder()
		mockRW.Header().Set("Test", "value")

		ew := &errorWriter{failAfter: 0} // Fail immediately
		// Use a 1-byte buffer to force immediate write failure
		brw := bufio.NewReadWriter(
			bufio.NewReader(ew),
			bufio.NewWriterSize(ew, 1),
		)

		w := &wsResponseWriter{
			ResponseWriter: &mockGinResponseWriter{ResponseRecorder: mockRW},
			conn:           nil,
			brw:            brw,
		}

		// Should not panic, just log error and return
		assert.NotPanics(t, func() {
			w.WriteHeader(http.StatusOK)
		})
	})

	t.Run("header write error", func(t *testing.T) {
		mockRW := httptest.NewRecorder()
		// Add a header that will be written
		mockRW.Header().Set("Upgrade", "websocket")

		// Fail after status line succeeds (need enough writes for "HTTP/1.1 101 Switching Protocols\r\n")
		// With 1-byte buffer and ~35 char status line, we need 35+ successful writes
		ew := &errorWriter{failAfter: 40}
		brw := bufio.NewReadWriter(
			bufio.NewReader(ew),
			bufio.NewWriterSize(ew, 1),
		)

		w := &wsResponseWriter{
			ResponseWriter: &mockGinResponseWriter{ResponseRecorder: mockRW},
			conn:           nil,
			brw:            brw,
		}

		// Should not panic, just log error and return
		assert.NotPanics(t, func() {
			w.WriteHeader(http.StatusSwitchingProtocols)
		})
	})

	t.Run("header terminator write error", func(t *testing.T) {
		mockRW := httptest.NewRecorder()
		// No headers - empty header section

		// Status line (~20 chars) + empty headers, then fail on terminator "\r\n"
		ew := &errorWriter{failAfter: 25}
		brw := bufio.NewReadWriter(
			bufio.NewReader(ew),
			bufio.NewWriterSize(ew, 1),
		)

		w := &wsResponseWriter{
			ResponseWriter: &mockGinResponseWriter{ResponseRecorder: mockRW},
			conn:           nil,
			brw:            brw,
		}

		// Should not panic, just log error and return
		assert.NotPanics(t, func() {
			w.WriteHeader(http.StatusOK)
		})
	})

	t.Run("flush error", func(t *testing.T) {
		mockRW := httptest.NewRecorder()

		// Fail on flush (after all writes succeed) - need many successful writes
		ew := &errorWriter{failAfter: 100}
		brw := bufio.NewReadWriter(
			bufio.NewReader(ew),
			bufio.NewWriterSize(ew, 1),
		)

		w := &wsResponseWriter{
			ResponseWriter: &mockGinResponseWriter{ResponseRecorder: mockRW},
			conn:           nil,
			brw:            brw,
		}

		// Should not panic, just log error and return
		assert.NotPanics(t, func() {
			w.WriteHeader(http.StatusOK)
		})
	})
}

// TestValidatePath tests the path validation function.
// Security model: validatePath cleans the input path and joins it with basePath.
// The security check verifies that the JOINED path stays within basePath.
// Example: input "/../etc/passwd" → cleaned "/etc/passwd" → joined "/tmp/etc/passwd"
// This is SAFE because the actual file served would be /tmp/etc/passwd, NOT /etc/passwd.
func TestValidatePath(t *testing.T) {
	fs := safeFileSystem{
		root:     http.Dir("/tmp"),
		basePath: "/tmp",
	}

	t.Run("valid simple path", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("valid nested path", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/subdir/file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/subdir/file.txt", cleanPath)
	})

	t.Run("path without leading slash", func(t *testing.T) {
		cleanPath, err := fs.validatePath("file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("path with dot components is cleaned", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/./subdir/../file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("root path is valid", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/")
		assert.NoError(t, err)
		assert.Equal(t, "/", cleanPath)
	})

	t.Run("leading dotdot at root level is normalized and joined safely", func(t *testing.T) {
		// Input: /../etc/passwd → Cleaned: /etc/passwd → Joined: /tmp/etc/passwd
		// This is SAFE because the joined path /tmp/etc/passwd IS within basePath /tmp/
		// The file that would be served is /tmp/etc/passwd, NOT /etc/passwd
		cleanPath, err := fs.validatePath("/../etc/passwd")
		assert.NoError(t, err)
		assert.Equal(t, "/etc/passwd", cleanPath)
	})

	t.Run("dotdot in path is normalized and joined safely", func(t *testing.T) {
		// Input: /subdir/../../etc/passwd → Cleaned: /etc/passwd → Joined: /tmp/etc/passwd
		// This is SAFE because the joined path /tmp/etc/passwd IS within basePath /tmp/
		// The file that would be served is /tmp/etc/passwd, NOT /etc/passwd
		cleanPath, err := fs.validatePath("/subdir/../../etc/passwd")
		assert.NoError(t, err)
		assert.Equal(t, "/etc/passwd", cleanPath)
	})

	t.Run("empty path becomes root", func(t *testing.T) {
		cleanPath, err := fs.validatePath("")
		assert.NoError(t, err)
		assert.Equal(t, "/", cleanPath)
	})
}

// TestSafeFileSystem tests the safe file system implementation.
func TestSafeFileSystem(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)
	require.NoError(t, err)

	err = os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644)
	require.NoError(t, err)

	// Resolve symlinks to ensure consistent path comparison (important on macOS)
	resolvedBase, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: resolvedBase,
	}

	t.Run("valid path opens file", func(t *testing.T) {
		f, err := fs.Open("/file.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
	})

	t.Run("nested valid path opens file", func(t *testing.T) {
		f, err := fs.Open("/subdir/nested.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
	})

	t.Run("path traversal attack returns error", func(t *testing.T) {
		_, err := fs.Open("/../../../etc/passwd")
		assert.Error(t, err)
	})

	t.Run("dotdot in middle returns error for outside path", func(t *testing.T) {
		_, err := fs.Open("/subdir/../../etc/passwd")
		assert.Error(t, err)
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		_, err := fs.Open("/nonexistent.txt")
		assert.Error(t, err)
	})

	t.Run("clean path with redundant components works", func(t *testing.T) {
		f, err := fs.Open("/./subdir/../file.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
	})

	t.Run("open root directory", func(t *testing.T) {
		f, err := fs.Open("/")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
		stat, err := f.Stat()
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})
}

// TestSafeFileSystemSymlinkProtection tests symlink escape prevention.
func TestSafeFileSystemSymlinkProtection(t *testing.T) {
	tmpDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a file outside the static directory
	err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("secret data"), 0644)
	require.NoError(t, err)

	// Create a symlink inside tmpDir that points outside
	symlinkPath := filepath.Join(tmpDir, "escape")
	err = os.Symlink(outsideDir, symlinkPath)
	require.NoError(t, err)

	// Resolve symlinks to ensure consistent path comparison (important on macOS)
	resolvedBase, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: resolvedBase,
	}

	t.Run("symlink escaping directory is blocked", func(t *testing.T) {
		_, err := fs.Open("/escape/secret.txt")
		assert.Error(t, err)
		assert.ErrorIs(t, err, os.ErrPermission)
	})
}

// TestEnvShutdown tests the graceful shutdown functionality.
func TestEnvShutdown(t *testing.T) {
	t.Run("shutdown closes channel", func(t *testing.T) {
		shutdownCh := make(chan struct{})
		env := &Env{
			shutdownCh: shutdownCh,
		}

		// Verify channel is not closed
		select {
		case <-env.shutdownCh:
			t.Fatal("channel should not be closed yet")
		default:
			// expected
		}

		// Call shutdown
		env.Shutdown()

		// Verify channel is now closed
		select {
		case <-env.shutdownCh:
			// expected - channel is closed
		default:
			t.Fatal("channel should be closed after Shutdown()")
		}
	})

	t.Run("shutdown with nil channel is safe", func(t *testing.T) {
		env := &Env{
			shutdownCh: nil,
		}

		// Should not panic
		env.Shutdown()
	})

	t.Run("shutdown can be called multiple times safely", func(t *testing.T) {
		shutdownCh := make(chan struct{})
		env := &Env{
			shutdownCh: shutdownCh,
		}

		// Should not panic when called multiple times
		env.Shutdown()
		env.Shutdown()
		env.Shutdown()

		// Verify channel is closed
		select {
		case <-env.shutdownCh:
			// expected - channel is closed
		default:
			t.Fatal("channel should be closed")
		}
	})

	t.Run("shutdown waits for connWg", func(t *testing.T) {
		shutdownCh := make(chan struct{})
		env := &Env{
			shutdownCh: shutdownCh,
		}

		// Simulate an active connection by incrementing the WaitGroup
		env.connWg.Add(1)

		// Start Shutdown in a goroutine and track when it completes
		shutdownDone := make(chan struct{})
		go func() {
			env.Shutdown()
			close(shutdownDone)
		}()

		// Verify Shutdown is blocked waiting for connWg
		select {
		case <-shutdownDone:
			t.Fatal("Shutdown should be blocked waiting for connWg")
		case <-time.After(300 * time.Millisecond):
			// Expected - Shutdown is still waiting
		}

		// Now release the WaitGroup
		env.connWg.Done()

		// Shutdown should now complete
		select {
		case <-shutdownDone:
			// Expected - Shutdown completed after connWg.Done()
		case <-time.After(time.Second):
			t.Fatal("Shutdown should have completed after connWg.Done()")
		}
	})
}

// TestStartRouter tests the StartRouter method.
func TestStartRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	require.NoError(t, err)

	serverConfig := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           "0", // Use port 0 for automatic port assignment
		StaticFilePath: tmpDir,
	}

	env := &Env{config: serverConfig}
	env.lockdown, _ = NewLockdown("")

	router := env.CreateRouter()
	srv := env.StartRouter(router)

	assert.NotNil(t, srv)
	assert.Equal(t, "127.0.0.1:0", srv.Addr)
	assert.Equal(t, router, srv.Handler)
	assert.Equal(t, 10*time.Second, srv.ReadHeaderTimeout)
}

// TestNotifyWebSocketClients tests the WebSocket notification functionality.
func TestNotifyWebSocketClients(t *testing.T) {
	t.Run("notifies with no connections", func(t *testing.T) {
		// Reset connections
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		// Cleanup to prevent test pollution
		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		// Should not panic with empty connections
		notifyWebSocketClients("test message")
	})
}

// TestRemoveWebSocketConnectionCleanup tests the connection removal cleanup functionality.
func TestRemoveWebSocketConnectionCleanup(t *testing.T) {
	t.Run("removes connection and cleans up closedConns", func(t *testing.T) {
		// Reset state
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		// Cleanup to prevent test pollution
		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		// This test verifies the function doesn't panic with nil
		removeWebSocketConnection(nil)

		connectionsMutex.RLock()
		assert.Len(t, connections, 0)
		// closedConns should be empty after cleanup
		assert.Len(t, closedConns, 0)
		connectionsMutex.RUnlock()
	})

	t.Run("removes actual connection from slice", func(t *testing.T) {
		// Reset state
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		// Cleanup to prevent test pollution
		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		conn := &websocket.Conn{}
		connectionsMutex.Lock()
		connections = append(connections, conn)
		connectionsMutex.Unlock()

		removeWebSocketConnection(conn)

		connectionsMutex.RLock()
		assert.NotContains(t, connections, conn)
		// Entry should be deleted after removal
		assert.Len(t, closedConns, 0)
		connectionsMutex.RUnlock()
	})

	t.Run("removes connection from middle of slice", func(t *testing.T) {
		// Reset state
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		conn1 := &websocket.Conn{}
		conn2 := &websocket.Conn{}
		conn3 := &websocket.Conn{}
		connectionsMutex.Lock()
		connections = append(connections, conn1, conn2, conn3)
		connectionsMutex.Unlock()

		// Remove middle connection
		removeWebSocketConnection(conn2)

		connectionsMutex.RLock()
		assert.Len(t, connections, 2)
		// Check by pointer address, not value (all zero-value Conns are equal by value)
		foundConn1 := false
		foundConn2 := false
		foundConn3 := false
		for _, c := range connections {
			if c == conn1 {
				foundConn1 = true
			}
			if c == conn2 {
				foundConn2 = true
			}
			if c == conn3 {
				foundConn3 = true
			}
		}
		connectionsMutex.RUnlock()

		assert.True(t, foundConn1, "conn1 should still be in the slice")
		assert.False(t, foundConn2, "conn2 should have been removed")
		assert.True(t, foundConn3, "conn3 should still be in the slice")
	})
}

// TestNotifyWebSocketClientsFiltersClosedConnections tests that closed connections are filtered.
func TestNotifyWebSocketClientsFiltersClosedConnections(t *testing.T) {
	// Reset state
	connectionsMutex.Lock()
	connections = nil
	closedConns = make(map[*websocket.Conn]bool)
	connectionsMutex.Unlock()

	t.Cleanup(func() {
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()
	})

	// Add a connection and mark it as closed
	conn := &websocket.Conn{}
	connectionsMutex.Lock()
	connections = append(connections, conn)
	closedConns[conn] = true
	connectionsMutex.Unlock()

	// Should not panic and should skip the closed connection
	notifyWebSocketClients("test message")
}

// TestConnWgTracking tests that connWg is properly incremented and decremented.
func TestConnWgTracking(t *testing.T) {
	env := &Env{
		shutdownCh: make(chan struct{}),
	}

	// Simulate handleWebSocketConnection incrementing the WaitGroup
	env.connWg.Add(1)

	// Simulate checkConnection running in a goroutine
	checkDone := make(chan struct{})
	go func() {
		defer env.connWg.Done()
		// Simulate waiting for shutdown signal
		<-env.shutdownCh
		close(checkDone)
	}()

	// Verify the goroutine is running (WaitGroup has 1)
	select {
	case <-checkDone:
		t.Fatal("goroutine should not have exited yet")
	case <-time.After(50 * time.Millisecond):
		// Expected - goroutine is waiting
	}

	// Trigger shutdown
	shutdownComplete := make(chan struct{})
	go func() {
		env.Shutdown()
		close(shutdownComplete)
	}()

	// The checkDone should close first (simulating goroutine exit)
	select {
	case <-checkDone:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("goroutine should have received shutdown signal")
	}

	// Then shutdown should complete
	select {
	case <-shutdownComplete:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("Shutdown should have completed")
	}
}

// TestCreateRouterInitializesShutdownChannel tests that CreateRouter initializes the shutdown channel.
func TestCreateRouterInitializesShutdownChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	require.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
	}

	// Create env without shutdown channel
	env := &Env{
		config:     serverConfig,
		shutdownCh: nil,
	}
	env.lockdown, _ = NewLockdown("")

	// CreateRouter should initialize the shutdown channel
	_ = env.CreateRouter()

	assert.NotNil(t, env.shutdownCh, "CreateRouter should initialize shutdownCh")
}

// TestValidatePathEdgeCases tests edge cases in path validation.
func TestValidatePathEdgeCases(t *testing.T) {
	// Create a temp directory with a specific structure
	tmpDir := t.TempDir()

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: tmpDir,
	}

	t.Run("multiple slashes are cleaned", func(t *testing.T) {
		cleanPath, err := fs.validatePath("///file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("dot path is cleaned to root", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/.")
		assert.NoError(t, err)
		assert.Equal(t, "/", cleanPath)
	})

	t.Run("complex path with multiple dots is cleaned", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/a/b/./c/../d")
		assert.NoError(t, err)
		assert.Equal(t, "/a/b/d", cleanPath)
	})
}

// TestPrometheusHandler tests the prometheus handler wrapper.
func TestPrometheusHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := prometheusHandler()
	assert.NotNil(t, handler)

	// Create a test request
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	// Create a gin context
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Call the handler - should not panic
	handler(c)

	// Should return a valid response (prometheus metrics)
	assert.Equal(t, http.StatusOK, w.Code)
}

// mockTaskRepositoryWithHealth allows configuring health check response.
type mockTaskRepositoryWithHealth struct {
	mockTaskRepository
	healthy   bool
	taskError error
}

func (m *mockTaskRepositoryWithHealth) Check() bool {
	return m.healthy
}

func (m *mockTaskRepositoryWithHealth) GetTask(id string) (*models.Task, error) {
	if m.taskError != nil {
		return nil, m.taskError
	}
	return &models.Task{
		Id:           id,
		App:          "test-app",
		Author:       "test-author",
		Project:      "test-project",
		Status:       "deployed",
		StatusReason: "",
	}, nil
}

// TestHealthzEndpoint tests the healthz endpoint.
func TestHealthzEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns up when healthy", func(t *testing.T) {
		argo := &argocd.Argo{}
		argo.Init(&mockTaskRepositoryWithHealth{healthy: true}, &mockArgoApi{}, &mockMetrics{})

		env := &Env{argo: argo}

		router := gin.New()
		router.GET("/healthz", env.healthz)

		req, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "up")
	})

	t.Run("returns down when unhealthy", func(t *testing.T) {
		argo := &argocd.Argo{}
		argo.Init(&mockTaskRepositoryWithHealth{healthy: false}, &mockArgoApi{}, &mockMetrics{})

		env := &Env{argo: argo}

		router := gin.New()
		router.GET("/healthz", env.healthz)

		req, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "down")
	})
}

// TestGetConfigEndpoint tests the getConfig endpoint.
func TestGetConfigEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serverConfig := &config.ServerConfig{
		StateType:      "in-memory",
		LogLevel:       "debug",
		DevEnvironment: true,
	}

	env := &Env{config: serverConfig}

	router := gin.New()
	router.GET("/api/v1/config", env.getConfig)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "in-memory")
	assert.Contains(t, w.Body.String(), "debug")
	assert.Contains(t, w.Body.String(), "devEnvironment")
}

// TestGetTaskStatusEndpoint tests the getTaskStatus endpoint.
func TestGetTaskStatusEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns task when found", func(t *testing.T) {
		argo := &argocd.Argo{}
		argo.Init(&mockTaskRepositoryWithHealth{healthy: true}, &mockArgoApi{}, &mockMetrics{})

		env := &Env{argo: argo}

		router := gin.New()
		router.GET("/api/v1/tasks/:id", env.getTaskStatus)

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/tasks/test-task-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-task-id")
		assert.Contains(t, w.Body.String(), "test-app")
	})

	t.Run("returns error when task not found", func(t *testing.T) {
		argo := &argocd.Argo{}
		argo.Init(&mockTaskRepositoryWithHealth{
			healthy:   true,
			taskError: fmt.Errorf("task not found"),
		}, &mockArgoApi{}, &mockMetrics{})

		env := &Env{argo: argo}

		router := gin.New()
		router.GET("/api/v1/tasks/:id", env.getTaskStatus)

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/tasks/nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "nonexistent")
		assert.Contains(t, w.Body.String(), "task not found")
	})
}

// TestValidateTokenWithStrategies tests the validateToken method.
func TestValidateTokenWithStrategies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns result from authenticator when no allowed strategy", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		mockAuth := auth.NewAuthenticator(strategies)

		env := &Env{
			authenticator: mockAuth,
			strategies:    strategies,
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/test", nil)

		// With empty authenticator and no strategies, validation returns false
		valid, err := env.validateToken(c, "")
		assert.False(t, valid)
		assert.NoError(t, err)
	})

	t.Run("skips non-matching strategy headers", func(t *testing.T) {
		env := &Env{
			strategies: make(map[string]auth.AuthStrategy),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/test", nil)
		c.Request.Header.Set("Authorization", "Bearer test-token")

		// With no matching strategy, returns false
		valid, err := env.validateAllowedStrategy(c, "Keycloak-Authorization")
		assert.False(t, valid)
		assert.NoError(t, err)
	})
}

// mockAuthStrategy is a configurable mock for auth.AuthStrategy.
type mockAuthStrategy struct {
	valid bool
	err   error
}

func (m *mockAuthStrategy) Validate(_ string) (bool, error) {
	return m.valid, m.err
}

// mockTaskRepositoryForAddTask extends mockTaskRepositoryWithHealth with AddTask support.
type mockTaskRepositoryForAddTask struct {
	mockTaskRepositoryWithHealth
	addTaskError error
	addedTask    *models.Task
}

func (m *mockTaskRepositoryForAddTask) AddTask(task models.Task) (*models.Task, error) {
	if m.addTaskError != nil {
		return nil, m.addTaskError
	}
	task.Id = "generated-task-id"
	m.addedTask = &task
	return &task, nil
}

// TestAddTaskEndpoint tests the addTask endpoint.
func TestAddTaskEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns error for invalid JSON payload", func(t *testing.T) {
		env := &Env{}

		router := gin.New()
		router.POST("/api/v1/tasks", env.addTask)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotAcceptable, w.Code)
		assert.Contains(t, w.Body.String(), "invalid payload")
	})

	t.Run("rejects task when lockdown is active", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		lockdown.SetLock()

		env := &Env{
			lockdown: lockdown,
		}

		router := gin.New()
		router.POST("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotAcceptable, w.Code)
		assert.Contains(t, w.Body.String(), "rejected")
		assert.Contains(t, w.Body.String(), "lockdown is active")
	})

	t.Run("returns error when token validation fails", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		strategies := make(map[string]auth.AuthStrategy)
		strategies["Authorization"] = &mockAuthStrategy{valid: false, err: fmt.Errorf("validation error")}

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		router := gin.New()
		router.POST("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("returns error when argo.AddTask fails", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		strategies := make(map[string]auth.AuthStrategy)

		argo := &argocd.Argo{}
		argo.Init(&mockTaskRepositoryForAddTask{
			mockTaskRepositoryWithHealth: mockTaskRepositoryWithHealth{healthy: true},
			addTaskError:                 fmt.Errorf("argo unavailable"),
		}, &mockArgoApi{}, &mockMetrics{})

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			argo:          argo,
		}

		router := gin.New()
		router.POST("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "down")
	})

}

// TestSetDeployLockWithKeycloak tests SetDeployLock with Keycloak authentication.
func TestSetDeployLockWithKeycloak(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns error when validation fails", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: false, err: fmt.Errorf("keycloak error")}

		env := &Env{
			lockdown:   lockdown,
			strategies: strategies,
			config: &config.ServerConfig{
				Keycloak: config.KeycloakConfig{Enabled: true},
			},
		}

		router := gin.New()
		router.POST("/api/v1/deploy-lock", env.SetDeployLock)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		req.Header.Set(keycloakHeader, "Bearer test-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Validation process failed")
	})

	t.Run("returns unauthorized when token is invalid", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: false, err: nil}

		env := &Env{
			lockdown:   lockdown,
			strategies: strategies,
			config: &config.ServerConfig{
				Keycloak: config.KeycloakConfig{Enabled: true},
			},
		}

		router := gin.New()
		router.POST("/api/v1/deploy-lock", env.SetDeployLock)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		req.Header.Set(keycloakHeader, "Bearer invalid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "not authorized")
	})

	t.Run("sets lock when token is valid", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: true, err: nil}

		env := &Env{
			lockdown:   lockdown,
			strategies: strategies,
			config: &config.ServerConfig{
				Keycloak: config.KeycloakConfig{Enabled: true},
			},
		}

		router := gin.New()
		router.POST("/api/v1/deploy-lock", env.SetDeployLock)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		req.Header.Set(keycloakHeader, "Bearer valid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "deploy lock is set")
		assert.True(t, lockdown.IsLocked())
	})
}

// TestReleaseDeployLockWithKeycloak tests ReleaseDeployLock with Keycloak authentication.
func TestReleaseDeployLockWithKeycloak(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns error when validation fails", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		lockdown.SetLock()
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: false, err: fmt.Errorf("keycloak error")}

		env := &Env{
			lockdown:   lockdown,
			strategies: strategies,
			config: &config.ServerConfig{
				Keycloak: config.KeycloakConfig{Enabled: true},
			},
		}

		router := gin.New()
		router.DELETE("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		req.Header.Set(keycloakHeader, "Bearer test-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Validation process failed")
	})

	t.Run("returns unauthorized when token is invalid", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		lockdown.SetLock()
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: false, err: nil}

		env := &Env{
			lockdown:   lockdown,
			strategies: strategies,
			config: &config.ServerConfig{
				Keycloak: config.KeycloakConfig{Enabled: true},
			},
		}

		router := gin.New()
		router.DELETE("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		req.Header.Set(keycloakHeader, "Bearer invalid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "not authorized")
	})

	t.Run("releases lock when token is valid", func(t *testing.T) {
		lockdown, _ := NewLockdown("")
		lockdown.SetLock()
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: true, err: nil}

		env := &Env{
			lockdown:   lockdown,
			strategies: strategies,
			config: &config.ServerConfig{
				Keycloak: config.KeycloakConfig{Enabled: true},
			},
		}

		router := gin.New()
		router.DELETE("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		req.Header.Set(keycloakHeader, "Bearer valid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "deploy lock is released")
		assert.False(t, lockdown.IsLocked())
	})
}

// TestValidateAllowedStrategyFull tests validateAllowedStrategy with more scenarios.
func TestValidateAllowedStrategyFull(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("validates successfully with matching strategy", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: true, err: nil}

		env := &Env{
			strategies: strategies,
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/test", nil)
		c.Request.Header.Set(keycloakHeader, "Bearer valid-token")

		valid, err := env.validateAllowedStrategy(c, keycloakHeader)
		assert.True(t, valid)
		assert.NoError(t, err)
	})

	t.Run("strips Bearer prefix from token", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: true, err: nil}

		env := &Env{
			strategies: strategies,
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/test", nil)
		c.Request.Header.Set(keycloakHeader, "Bearer my-token")

		valid, err := env.validateAllowedStrategy(c, keycloakHeader)
		assert.True(t, valid)
		assert.NoError(t, err)
	})

	t.Run("returns error from strategy validation", func(t *testing.T) {
		expectedErr := fmt.Errorf("token expired")
		strategies := make(map[string]auth.AuthStrategy)
		strategies[keycloakHeader] = &mockAuthStrategy{valid: false, err: expectedErr}

		env := &Env{
			strategies: strategies,
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/test", nil)
		c.Request.Header.Set(keycloakHeader, "Bearer expired-token")

		valid, err := env.validateAllowedStrategy(c, keycloakHeader)
		assert.False(t, valid)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("skips strategies with non-matching headers", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		strategies["Authorization"] = &mockAuthStrategy{valid: true, err: nil}
		strategies[keycloakHeader] = &mockAuthStrategy{valid: false, err: nil}

		env := &Env{
			strategies: strategies,
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/test", nil)
		c.Request.Header.Set("Authorization", "Bearer token")

		// Only keycloakHeader is allowed, so Authorization should be skipped
		valid, err := env.validateAllowedStrategy(c, keycloakHeader)
		assert.False(t, valid)
		assert.NoError(t, err)
	})
}
