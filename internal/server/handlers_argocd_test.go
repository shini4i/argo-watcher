package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/mocks"
)

// TestArgoStatus verifies the read-only reachability endpoint reflects the
// cached ArgoCD availability as a bare JSON boolean.
func TestArgoStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	serve := func(argo *argocd.Argo) *httptest.ResponseRecorder {
		env := &Env{argo: argo}
		router := gin.New()
		router.GET("/api/v1/argocd-status", env.argoStatus)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd-status", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("reports available by default", func(t *testing.T) {
		argo := &argocd.Argo{}
		argo.Init(mocks.NewMockTaskRepository(ctrl), newArgoAPI(ctrl), newMetrics(ctrl))

		w := serve(argo)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Body.String())
	})

	t.Run("reports unavailable after a failed check", func(t *testing.T) {
		repo := mocks.NewMockTaskRepository(ctrl)
		repo.EXPECT().Check().Return(false)
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		_, err := argo.Check()
		assert.Error(t, err)

		w := serve(argo)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "false", w.Body.String())
	})
}

// TestArgoStatusEndpointRegistration verifies the read-only reachability route
// is registered unconditionally (unlike the Keycloak-gated deploy-lock writes),
// so the frontend banner can always bootstrap its state.
func TestArgoStatusEndpointRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hasRoute := func(routes gin.RoutesInfo, method, path string) bool {
		for _, r := range routes {
			if r.Method == method && r.Path == path {
				return true
			}
		}
		return false
	}

	newRouter := func(t *testing.T, keycloakEnabled bool) *gin.Engine {
		t.Helper()
		serverConfig := &config.ServerConfig{
			StaticFilePath: t.TempDir(),
			Keycloak:       config.KeycloakConfig{Enabled: keycloakEnabled},
		}
		env := &Env{config: serverConfig}
		var err error
		env.lockdown, err = NewLockdown("")
		require.NoError(t, err)
		return env.CreateRouter()
	}

	const statusPath = "/api/v1/argocd-status"

	for _, keycloakEnabled := range []bool{false, true} {
		routes := newRouter(t, keycloakEnabled).Routes()
		assert.True(t, hasRoute(routes, http.MethodGet, statusPath),
			"GET argocd-status must be registered regardless of Keycloak (enabled=%v)", keycloakEnabled)
	}
}

// TestStartArgoWatcher verifies the watcher goroutine is tracked by connWg and
// exits when the shutdown channel closes, so graceful shutdown neither hangs nor
// leaks it.
func TestStartArgoWatcher(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	argo := &argocd.Argo{}
	argo.Init(mocks.NewMockTaskRepository(ctrl), newArgoAPI(ctrl), newMetrics(ctrl))

	env := &Env{argo: argo, shutdownCh: make(chan struct{})}
	env.StartArgoWatcher()

	close(env.shutdownCh)

	done := make(chan struct{})
	go func() {
		env.connWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// goroutine exited and released its connWg slot
	case <-time.After(2 * time.Second):
		t.Fatal("StartArgoWatcher goroutine did not exit after shutdown")
	}
}

// TestWatchArgoTransitions verifies the watcher pushes a message only when the
// observed reachability actually changes, and that it stops on request.
func TestWatchArgoTransitions(t *testing.T) {
	recv := func(t *testing.T, msgs <-chan string) string {
		t.Helper()
		select {
		case m := <-msgs:
			return m
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for notification")
			return ""
		}
	}

	t.Run("notifies on reachability transitions", func(t *testing.T) {
		var available atomic.Bool
		available.Store(true)
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 4)
		go watchArgoTransitions(stop, 5*time.Millisecond, available.Load, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its baseline

		available.Store(false)
		assert.Equal(t, argoDownMessage, recv(t, msgs))

		available.Store(true)
		assert.Equal(t, argoUpMessage, recv(t, msgs))
	})

	t.Run("does not notify without a transition", func(t *testing.T) {
		var available atomic.Bool
		available.Store(true)
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 1)
		go watchArgoTransitions(stop, 5*time.Millisecond, available.Load, func(m string) { msgs <- m })

		select {
		case m := <-msgs:
			t.Fatalf("unexpected notification: %q", m)
		case <-time.After(50 * time.Millisecond):
			// no transition occurred, so no notification is expected
		}
	})

	t.Run("stops when stop channel is closed", func(t *testing.T) {
		var available atomic.Bool
		available.Store(true)
		stop := make(chan struct{})
		done := make(chan struct{})

		go func() {
			watchArgoTransitions(stop, 5*time.Millisecond, available.Load, func(string) {})
			close(done)
		}()

		close(stop)

		select {
		case <-done:
			// returned as expected
		case <-time.After(time.Second):
			t.Fatal("watchArgoTransitions did not return after stop was closed")
		}
	})
}
