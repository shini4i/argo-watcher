package server

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/config"
)

func TestNewServer_Success(t *testing.T) {
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   config.URL{URL: *argoURL},
		ArgoToken: "test-token",
		StateType: "in-memory",
	}

	s, err := NewServer(cfg, reg)

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, cfg, s.env.config)
	// A zero drainDelay silently skips the readiness-propagation phase of shutdown,
	// and every other test constructs Server directly — so this is the only place a
	// dropped wiring line would be caught before the lab.
	assert.Equal(t, readinessDrainDelay, s.drainDelay)
	assert.Nil(t, s.closeLockPool, "in-memory state opens no advisory-lock pool")
}

// A Postgres-backed server must build its locker on a pool of its own. Sharing the
// state's — now capped — pool deadlocks: a lock holder keeps its connection for the
// whole write-back while that write-back queries the state, so enough concurrent
// holders occupy every connection the queries they are waiting on need.
func TestNewServer_PostgresBuildsADedicatedLockPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	s, err := NewServer(&config.ServerConfig{
		ArgoUrl:   config.URL{URL: *argoURL},
		ArgoToken: "test-token",
		StateType: "postgres",
		Db:        config.DatabaseConfig{DSN: dsn},
	}, prometheus.NewRegistry())

	require.NoError(t, err)
	require.NotNil(t, s.closeLockPool, "the advisory locker must own the pool it holds connections from")
	t.Cleanup(func() {
		s.probeCancel()
		_ = s.closeLockPool()
	})
}

// A startup that fails after the advisory-lock pool is open must still close it.
// Only the returned Server owns that pool, so a caller retrying startup — a test
// suite, or a supervised restart in-process — would otherwise pile up connections.
func TestNewServer_PostgresClosesTheLockPoolWhenStartupFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	// Rejected by NewBatchConfig, which runs after the pool is created.
	t.Setenv("GIT_BATCH_WRITEBACK", "true")
	t.Setenv("GIT_BATCH_MAX_SIZE", "0")

	s, err := NewServer(&config.ServerConfig{
		ArgoUrl:   config.URL{URL: *argoURL},
		ArgoToken: "test-token",
		StateType: "postgres",
		Db:        config.DatabaseConfig{DSN: dsn},
	}, prometheus.NewRegistry())

	require.Error(t, err)
	assert.Nil(t, s)
	// Names the failure that proves the pool was already open when it happened —
	// the window the deferred close covers. Fail earlier and this guards nothing.
	assert.Contains(t, err.Error(), "GIT_BATCH_MAX_SIZE")
}

func TestNewServer_StateInitFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   config.URL{URL: *argoURL},
		ArgoToken: "test-token",
		StateType: "invalid-state-type",
	}

	_, err = NewServer(cfg, reg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected state type received: invalid-state-type")
}

func TestNewServer_PostgresConnectionFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	t.Setenv("DB_DSN", "")

	cfg := &config.ServerConfig{
		ArgoUrl:   config.URL{URL: *argoURL},
		ArgoToken: "test-token",
		StateType: "postgres",
	}

	_, err = NewServer(cfg, reg)

	assert.Error(t, err)
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

	writebackDrainShare := shutdownBudget - (readinessDrainDelay + httpDrainBudget + shutdownTimeout)
	assert.GreaterOrEqual(t, writebackDrainShare, 5*time.Second,
		"the readiness delay and the HTTP and WebSocket drains must leave the batch write-back drain a usable share")
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

// The advisory-lock pool is closed last, so no drain phase is cut off mid-commit.
// Hoisting the close above the drains is the regression, hence the phase-1 probe.
func TestShutdown_ClosesTheLockPoolLast(t *testing.T) {
	env := &Env{shutdownCh: make(chan struct{})}
	closed := 0
	s := &Server{env: env, closeLockPool: func() error {
		closed++
		return nil
	}}

	var closedDuringHTTPDrain bool
	srv := &fakeHTTPShutdowner{onShutdown: func() { closedDuringHTTPDrain = closed > 0 }}

	s.shutdown(srv)

	assert.False(t, closedDuringHTTPDrain, "the pool must outlive every drain phase")
	assert.Equal(t, 1, closed, "the lock pool must be closed exactly once")
}

// In-memory deployments never build a lock pool, so shutdown must tolerate a nil
// closer, and a closer that fails must not derail the rest of the sequence.
func TestShutdown_SurvivesAMissingOrFailingLockPoolCloser(t *testing.T) {
	t.Run("nil closer", func(t *testing.T) {
		s := &Server{env: &Env{shutdownCh: make(chan struct{})}}

		assert.NotPanics(t, func() { s.shutdown(&fakeHTTPShutdowner{}) })
	})

	t.Run("closer returns an error", func(t *testing.T) {
		s := &Server{
			env:           &Env{shutdownCh: make(chan struct{})},
			closeLockPool: func() error { return errors.New("pool already closed") },
		}

		assert.NotPanics(t, func() { s.shutdown(&fakeHTTPShutdowner{}) })
	})
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

// TestShutdown_ReadinessFailsBeforeTheListenerCloses is the ordering the readiness
// probe exists for. Endpoint removal is asynchronous, so a pod that closes its
// listener the instant SIGTERM lands is still receiving proxied requests — every
// rolling update then ends in a tail of connection resets. Failing readiness first
// is what lets the endpoints controller pull the pod while it can still serve.
func TestShutdown_ReadinessFailsBeforeTheListenerCloses(t *testing.T) {
	env := &Env{shutdownCh: make(chan struct{})}
	s := &Server{env: env}

	var readyAtListenerClose bool
	srv := &fakeHTTPShutdowner{onShutdown: func() {
		readyAtListenerClose = !env.isDraining()
	}}

	s.shutdown(srv)

	assert.False(t, readyAtListenerClose, "readiness must already report down when the listener closes")
	assert.True(t, env.isDraining(), "the drain flag must outlive the shutdown sequence")
}

// TestShutdown_ReadinessDelayPrecedesTheHTTPDrain proves the propagation window is
// actually waited out rather than merely flagged. Flipping the probe and closing the
// listener in the same breath leaves the race untouched, and nothing else in the
// sequence would catch it.
func TestShutdown_ReadinessDelayPrecedesTheHTTPDrain(t *testing.T) {
	env := &Env{shutdownCh: make(chan struct{})}
	// A real readinessDrainDelay would add its full wall-clock cost to the suite; the
	// regression guarded here is "no wait at all", which any non-zero delay exposes.
	s := &Server{env: env, drainDelay: 50 * time.Millisecond}

	var elapsedAtShutdown time.Duration
	start := time.Now()
	srv := &fakeHTTPShutdowner{onShutdown: func() {
		elapsedAtShutdown = time.Since(start)
	}}

	s.shutdown(srv)

	assert.GreaterOrEqual(t, elapsedAtShutdown, s.drainDelay,
		"the HTTP drain must not start until the readiness change has had time to propagate")
}
