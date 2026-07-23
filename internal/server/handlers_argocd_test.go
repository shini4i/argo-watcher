package server

import (
	"fmt"
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
// cached availability and names the unreachable subsystem in its JSON body.
func TestArgoStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	serve := func(argo *argocd.Argo) *httptest.ResponseRecorder {
		env := &Env{argo: argo}
		router := gin.New()
		router.GET("/api/v1/reachability", env.reachability)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reachability", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("reports available by default", func(t *testing.T) {
		argo := &argocd.Argo{}
		argo.Init(mocks.NewMockTaskRepository(ctrl), newArgoAPI(ctrl), newMetrics(ctrl))

		w := serve(argo)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"available":true}`, w.Body.String())
	})

	t.Run("reports unavailable and names the state backend after a failed check", func(t *testing.T) {
		repo := mocks.NewMockTaskRepository(ctrl)
		repo.EXPECT().Check().Return(false)
		// ArgoCD stays reachable (newArgoAPI returns a logged-in user), so only
		// the state backend is down and the reason must be "database".
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		_, err := argo.Check()
		assert.Error(t, err)

		w := serve(argo)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"available":false,"reason":"database"}`, w.Body.String())
	})

	t.Run("reports unavailable and names ArgoCD after a failed login", func(t *testing.T) {
		repo := mocks.NewMockTaskRepository(ctrl)
		repo.EXPECT().Check().Return(true)
		// State backend is up but ArgoCD login fails, so only ArgoCD is down and
		// the reason must serialize as "argocd" at the HTTP layer.
		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("login boom"))
		argo := &argocd.Argo{}
		argo.Init(repo, api, newMetrics(ctrl))

		_, err := argo.Check()
		assert.Error(t, err)

		w := serve(argo)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"available":false,"reason":"argocd"}`, w.Body.String())
	})
}

// TestArgoStatusEndpointRegistration verifies the read-only reachability route
// is registered unconditionally (unlike the OIDC-gated deploy-lock writes),
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

	newRouter := func(t *testing.T, oidcEnabled bool) *gin.Engine {
		t.Helper()
		serverConfig := &config.ServerConfig{
			StaticFilePath: t.TempDir(),
			OIDC:           config.OIDCConfig{Enabled: oidcEnabled},
		}
		env := &Env{config: serverConfig}
		var err error
		env.lockdown, err = NewLockdown("")
		require.NoError(t, err)
		return env.CreateRouter()
	}

	const statusPath = "/api/v1/reachability"

	for _, oidcEnabled := range []bool{false, true} {
		routes := newRouter(t, oidcEnabled).Routes()
		assert.True(t, hasRoute(routes, http.MethodGet, statusPath),
			"GET reachability must be registered regardless of OIDC (enabled=%v)", oidcEnabled)
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

	// reasonSource is a concurrency-safe string holder standing in for
	// Argo.UnavailableReason; the watcher samples its load function.
	newReasonSource := func(initial string) (*atomic.Value, func() string) {
		var v atomic.Value
		v.Store(initial)
		return &v, func() string { return v.Load().(string) }
	}

	t.Run("notifies on reachability transitions and cause switches", func(t *testing.T) {
		reason, load := newReasonSource(argocd.ReasonNone)
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 4)
		go watchArgoTransitions(stop, 5*time.Millisecond, load, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its baseline

		// Reachable -> database down: down message carries the cause.
		reason.Store(argocd.ReasonDatabase)
		assert.Equal(t, argoDownMessage+":"+argocd.ReasonDatabase, recv(t, msgs))

		// database down -> both down: a cause switch also pushes an update so the
		// banner wording stays accurate.
		reason.Store(argocd.ReasonBoth)
		assert.Equal(t, argoDownMessage+":"+argocd.ReasonBoth, recv(t, msgs))

		// Recovery clears the banner with the plain up message.
		reason.Store(argocd.ReasonNone)
		assert.Equal(t, argoUpMessage, recv(t, msgs))
	})

	t.Run("does not notify without a transition", func(t *testing.T) {
		_, load := newReasonSource(argocd.ReasonNone)
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 1)
		go watchArgoTransitions(stop, 5*time.Millisecond, load, func(m string) { msgs <- m })

		select {
		case m := <-msgs:
			t.Fatalf("unexpected notification: %q", m)
		case <-time.After(50 * time.Millisecond):
			// no transition occurred, so no notification is expected
		}
	})

	t.Run("stops when stop channel is closed", func(t *testing.T) {
		_, load := newReasonSource(argocd.ReasonNone)
		stop := make(chan struct{})
		done := make(chan struct{})

		go func() {
			watchArgoTransitions(stop, 5*time.Millisecond, load, func(string) {})
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
