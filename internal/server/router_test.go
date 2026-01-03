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

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: tmpDir,
	}

	t.Run("valid path opens file", func(t *testing.T) {
		f, err := fs.Open("/file.txt")
		assert.NoError(t, err)
		assert.NotNil(t, f)
		f.Close()
	})

	t.Run("nested valid path opens file", func(t *testing.T) {
		f, err := fs.Open("/subdir/nested.txt")
		assert.NoError(t, err)
		assert.NotNil(t, f)
		f.Close()
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
		assert.NoError(t, err)
		assert.NotNil(t, f)
		f.Close()
	})
}
