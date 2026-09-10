package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsRegistryServer starts a server whose WebSocket clients are tracked by the
// returned Env alone, and gives back the ws:// URL of its /ws endpoint.
func wsRegistryServer(t *testing.T) (*Env, string) {
	t.Helper()

	env, _ := readAuthEnv(t, false, nil)
	env.config.DevEnvironment = true // accept the httptest origin
	server := httptest.NewServer(env.CreateRouter())

	t.Cleanup(func() {
		shutdownEnv(env)
		server.Close()
	})

	return env, "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
}

// dialAndHold opens a WebSocket, drains it, and keeps it open until the test ends.
// The drain matters: the server closes with a WebSocket closing handshake, which
// waits out its own timeout against a client that never reads.
func dialAndHold(t *testing.T, url string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)

	go func() {
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
		}
	}()

	// CloseNow, not Close: a graceful close waits for a reply frame the server's
	// write-only connection goroutine never sends.
	t.Cleanup(func() { _ = conn.CloseNow() })
}

// registered waits for the server to record the handshake, which completes on the
// client before the server adds the connection.
func registered(t *testing.T, env *Env, want int) {
	t.Helper()

	assert.Eventually(t, func() bool {
		return len(env.ws.snapshot()) == want
	}, 5*time.Second, time.Millisecond)
}

// TestWebSocketRegistryIsPerEnv pins that one server's clients are invisible to
// another running in the same process. A shared registry makes every broadcast
// reach both, and forces tests to reset global state by hand.
func TestWebSocketRegistryIsPerEnv(t *testing.T) {
	dialed, url := wsRegistryServer(t)
	idle, _ := wsRegistryServer(t)

	dialAndHold(t, url)

	registered(t, dialed, 1)
	assert.Empty(t, idle.ws.snapshot())
}

// TestWebSocketRegistryDrainsOnShutdown pins checkConnection's shutdown branch:
// once Shutdown returns, connWg has waited for every connection goroutine, so
// none may still be registered.
func TestWebSocketRegistryDrainsOnShutdown(t *testing.T) {
	env, url := wsRegistryServer(t)
	dialAndHold(t, url)
	registered(t, env, 1)

	shutdownEnv(env)

	assert.Empty(t, env.ws.snapshot())
}

// TestNotifyWebSocketClientsDropsFailedConnection pins the registry's only
// self-healing path: without it a dead socket is retained for the life of the
// process and every later broadcast waits out its write timeout.
func TestNotifyWebSocketClientsDropsFailedConnection(t *testing.T) {
	env, url := wsRegistryServer(t)
	dialAndHold(t, url)
	registered(t, env, 1)

	// Close the server side, so the next broadcast write to it fails at once.
	env.ws.snapshot()[0].CloseNow() //nolint:errcheck // the close is the point

	env.notifyWebSocketClients("unreachable")

	assert.Empty(t, env.ws.snapshot())
}

func TestWsRegistryAddAndRemove(t *testing.T) {
	var registry wsRegistry
	conn := &websocket.Conn{}

	registry.add(conn)
	assert.Equal(t, []*websocket.Conn{conn}, registry.snapshot())

	registry.remove(conn)
	assert.Empty(t, registry.snapshot())
}

func TestWsRegistryRemoveFromTheMiddle(t *testing.T) {
	var registry wsRegistry
	first, middle, last := &websocket.Conn{}, &websocket.Conn{}, &websocket.Conn{}
	registry.add(first)
	registry.add(middle)
	registry.add(last)

	registry.remove(middle)

	// Compared by pointer: zero-value connections are all equal by value.
	assert.Equal(t, []*websocket.Conn{first, last}, registry.snapshot())
}

func TestWsRegistryRemoveUnknownConnection(t *testing.T) {
	var registry wsRegistry
	conn := &websocket.Conn{}
	registry.add(conn)

	registry.remove(&websocket.Conn{})

	assert.Equal(t, []*websocket.Conn{conn}, registry.snapshot())
}

// TestWsRegistrySnapshotIsACopy pins that a broadcast iterating the snapshot is
// unaffected by a removal racing it. Removing the FIRST of two is what makes this
// fail on a snapshot that aliases the registry: the shift overwrites the index
// the snapshot already handed out.
func TestWsRegistrySnapshotIsACopy(t *testing.T) {
	var registry wsRegistry
	first, second := &websocket.Conn{}, &websocket.Conn{}
	registry.add(first)
	registry.add(second)

	taken := registry.snapshot()
	registry.remove(first)

	assert.Equal(t, []*websocket.Conn{first, second}, taken)
}

func TestWsRegistryConcurrentAccess(t *testing.T) {
	var registry wsRegistry
	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := &websocket.Conn{}
			registry.add(conn)
			registry.snapshot()
			registry.remove(conn)
		}()
	}

	wg.Wait()

	assert.Empty(t, registry.snapshot())
}

// TestNotifyWebSocketClientsWithNoConnections pins that a broadcast to an empty
// registry is a no-op rather than a panic: both watchers fire on every transition,
// including before any client has connected.
func TestNotifyWebSocketClientsWithNoConnections(t *testing.T) {
	env, _ := readAuthEnv(t, false, nil)

	assert.NotPanics(t, func() { env.notifyWebSocketClients("test message") })
}
