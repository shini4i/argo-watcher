package server

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/config"
)

func TestNewServer_Success(t *testing.T) {
	// Arrange
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "in-memory",
	}

	// Act
	s, err := NewServer(cfg, reg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, cfg, s.config)
}

func TestNewServer_StateInitFailure(t *testing.T) {
	// Arrange
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "invalid-state-type",
	}

	// Act
	_, err = NewServer(cfg, reg)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected state type received: invalid-state-type")
}

func TestNewServer_PostgresConnectionFailure(t *testing.T) {
	// Arrange
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	t.Setenv("DB_DSN", "")

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "postgres",
	}

	// Act
	_, err = NewServer(cfg, reg)

	// Assert
	assert.Error(t, err)
	// This is the corrected, more robust assertion.
	assert.Contains(t, err.Error(), "failed to connect to")
}

// TestShutdownBudgetFitsGracePeriod guards the invariant that makes the batch
// write-back drain reachable. The shutdown phases run in sequence, so the earlier
// two caps must leave the last phase a USABLE share, not merely fit inside the
// budget: "sum < budget" alone would still pass with a 1s remainder, which is the
// starvation this bound exists to prevent.
//
// The 5s floor is not enough for a fresh git attempt (GIT_OP_TIMEOUT defaults to
// 90s) — nothing inside a 25s budget could be. It is sized for what the drain can
// actually accomplish: letting a retry loop observe the drain signal, resolve its
// batch, and deliver every result so no deploying goroutine is left blocked.
func TestShutdownBudgetFitsGracePeriod(t *testing.T) {
	assert.Less(t, shutdownBudget, 30*time.Second, "budget must fit the default pod grace period")

	writebackDrainShare := shutdownBudget - (httpDrainBudget + shutdownTimeout)
	assert.GreaterOrEqual(t, writebackDrainShare, 5*time.Second,
		"HTTP and WebSocket drains must leave the batch write-back drain a usable share")
}

// fakeHTTPShutdowner stands in for *http.Server so the phase budgeting can be
// exercised without binding a listener. It records the deadline of the context it
// was handed — the only way to observe that phase 1 is capped by httpDrainBudget
// rather than being handed the whole shutdown budget.
type fakeHTTPShutdowner struct {
	called   bool
	deadline time.Time
	hasDDL   bool
	err      error
	// onShutdown runs inside Shutdown, so a test can observe the rest of the server's
	// state at the exact moment phase 1 executes. That is the only way to assert
	// phase ORDER rather than merely that each phase ran.
	onShutdown func()
}

func (f *fakeHTTPShutdowner) Shutdown(ctx context.Context) error {
	f.called = true
	f.deadline, f.hasDDL = ctx.Deadline()
	if f.onShutdown != nil {
		f.onShutdown()
	}
	return f.err
}

// TestShutdown_HTTPPhaseIsCappedBelowTheBudget proves the HTTP drain is handed its
// own httpDrainBudget-derived deadline instead of the whole shutdownBudget. Without
// the cap, a single slow handler could consume the entire budget and starve the
// WebSocket and git write-back phases that run after it — the exact starvation this
// sequence was restructured to prevent, and invisible in a test that only checked
// that Shutdown was called.
func TestShutdown_HTTPPhaseIsCappedBelowTheBudget(t *testing.T) {
	s := &Server{env: &Env{shutdownCh: make(chan struct{})}}
	srv := &fakeHTTPShutdowner{}

	start := time.Now()
	s.shutdown(srv)

	require.True(t, srv.called, "the HTTP drain must run")
	require.True(t, srv.hasDDL, "the HTTP drain must be bounded, not open-ended")

	// Compared with a one-second tolerance because `start` is sampled just before the
	// call, not at the moment the context is created. The regression this guards —
	// handing phase 1 the whole shutdownBudget — is 17s off, far outside that slop.
	share := srv.deadline.Sub(start)
	assert.InDelta(t, httpDrainBudget.Seconds(), share.Seconds(), 1,
		"the HTTP drain must be bounded by its own cap, not the whole budget")
	assert.Less(t, share, shutdownBudget, "the HTTP drain must leave budget for the later phases")
}

// TestShutdown_HTTPDrainPrecedesWebSocketDrain guards the phase order the sequence
// is built around: the listener must be closed before the WebSocket drain starts
// waiting on connWg. Reversed, a WebSocket handshake could still arrive and call
// connWg.Add(1) after that Wait has begun — a WaitGroup misuse that can panic on an
// already-terminating process. Every other assertion here passes regardless of order,
// so without this one a phase swap would land silently.
func TestShutdown_HTTPDrainPrecedesWebSocketDrain(t *testing.T) {
	env := &Env{shutdownCh: make(chan struct{})}
	s := &Server{env: env}

	var wsAlreadySignalled bool
	srv := &fakeHTTPShutdowner{onShutdown: func() {
		select {
		case <-env.shutdownCh:
			wsAlreadySignalled = true
		default:
		}
	}}

	s.shutdown(srv)

	assert.False(t, wsAlreadySignalled, "the WebSocket drain must not begin before the listener is closed")
}

// TestShutdown_WebSocketDrainRunsAfterHTTPDrainFailure covers the failure branch: a
// forced HTTP shutdown is logged but must NOT abort the sequence. Returning early
// there would skip the WebSocket drain and the git write-back drain — dropping
// queued commits because an unrelated HTTP request refused to finish.
//
// Only the WebSocket drain is actually observed, hence the name. The zero-value
// updater exercises the non-nil branch of the phase-3 guard without needing a
// batcher, but its Close is a no-op, so phase 3 running is not asserted here — the
// context handed to it is not reachable from this package (*ArgoStatusUpdater's git
// updater is unexported), and adding a seam for one assertion is not worth it.
func TestShutdown_WebSocketDrainRunsAfterHTTPDrainFailure(t *testing.T) {
	env := &Env{shutdownCh: make(chan struct{})}
	s := &Server{env: env, updater: &argocd.ArgoStatusUpdater{}}

	s.shutdown(&fakeHTTPShutdowner{err: errors.New("forced shutdown")})

	select {
	case <-env.shutdownCh:
	default:
		t.Fatal("the WebSocket drain must still run after the HTTP drain fails")
	}
}
