package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
)

// probeEnv builds an Env whose state backend reports the given reachability, which
// is the only dependency the readiness probe consults.
func probeEnv(t *testing.T, stateUp bool) *Env {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	repo.EXPECT().Check().Return(stateUp).AnyTimes()

	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

	return &Env{argo: argo}
}

// probeGet serves a request against a router carrying only the probe routes, so the
// assertions cover the handlers rather than the surrounding middleware.
func probeGet(t *testing.T, env *Env, path string) *httptest.ResponseRecorder {
	t.Helper()

	router := chi.NewRouter()
	router.Get("/livez", env.livez)
	router.Get("/readyz", env.readyz)

	req, err := http.NewRequest(http.MethodGet, path, http.NoBody)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

// TestLivenessIgnoresTheStateBackend is the whole reason the probes are split. A
// liveness probe may only report conditions a restart can fix, and restarting the
// process does not bring the database back. Wiring the state-backend check to
// liveness turns a recoverable database outage into a fleet-wide CrashLoopBackoff
// while every replica is still perfectly able to serve task history and the
// unreachable banner.
func TestLivenessIgnoresTheStateBackend(t *testing.T) {
	env := probeEnv(t, false)

	recorder := probeGet(t, env, "/livez")

	assert.Equal(t, http.StatusOK, recorder.Code, "an unreachable state backend must not fail liveness")
	assert.Contains(t, recorder.Body.String(), "up")
}

// TestLivenessSurvivesDraining keeps liveness answering through graceful shutdown.
// Failing it there would invite the kubelet to restart a container that is
// deliberately winding down, killing the write-back drain mid-flush.
func TestLivenessSurvivesDraining(t *testing.T) {
	env := probeEnv(t, true)
	env.beginDraining()

	assert.Equal(t, http.StatusOK, probeGet(t, env, "/livez").Code)
}

func TestReadinessReportsTheStateBackend(t *testing.T) {
	t.Run("up when the state backend answers", func(t *testing.T) {
		recorder := probeGet(t, probeEnv(t, true), "/readyz")

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "up")
	})

	t.Run("down when the state backend is unreachable", func(t *testing.T) {
		recorder := probeGet(t, probeEnv(t, false), "/readyz")

		assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		assert.Contains(t, recorder.Body.String(), reasonStateUnreachable)
	})
}

// TestReadinessFailsWhileDraining covers the signal the split exists to add: once
// shutdown begins the pod must be pulled from the Service before its listener
// closes, otherwise every rolling update ends with a tail of connection resets sent
// to a pod that has already stopped accepting.
func TestReadinessFailsWhileDraining(t *testing.T) {
	env := probeEnv(t, true)
	env.beginDraining()

	recorder := probeGet(t, env, "/readyz")

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), reasonDraining,
		"an operator curling the endpoint mid-incident must be able to tell a drain from an outage")
}

// TestProbeRoutesAreRegistered guards the wiring: the handlers above are useless if
// CreateRouter never exposes them.
func TestProbeRoutesAreRegistered(t *testing.T) {
	env, _ := readAuthEnv(t, false, nil)

	for _, path := range []string{"/livez", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, path, http.NoBody)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			env.CreateRouter().ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}
