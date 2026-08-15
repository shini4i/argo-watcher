package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	connectionsMutex sync.RWMutex
	connections      []*websocket.Conn
	closedConns      = make(map[*websocket.Conn]bool) // Track closed connections to prevent use-after-close
)

const (
	// wsSubprotocol is the protocol the server negotiates. A browser fails the
	// connection unless the server echoes one of the protocols it offered, so this
	// gives it something to echo that is not the token below.
	wsSubprotocol = "argo-watcher.v1"

	// wsTokenSubprotocolPrefix carries a credential for clients that cannot set a
	// header on the handshake, which is every browser: the WebSocket API accepts only
	// a URL and a subprotocol list. A query parameter would be the other option, but
	// it lands in access logs.
	wsTokenSubprotocolPrefix = "argo-watcher.token."
)

// authorizeWebSocket reports whether the handshake may proceed, writing the rejection
// itself when it may not. With OIDC disabled it always passes.
//
// The socket broadcasts deployment-lock and Argo CD reachability transitions — the same
// signals GET /deploy-lock and /reachability require a credential for — so leaving it
// open would make protecting those endpoints cosmetic.
func (env *Env) authorizeWebSocket(w http.ResponseWriter, r *http.Request) bool {
	if !env.config.OIDC.Enabled {
		return true
	}

	valid, err := env.authenticator.AuthenticateRequest(r)
	if !valid && err == nil {
		// No header credential: fall back to the browser's transport.
		valid, err = env.authenticator.AuthenticateToken(oidcHeader, wsSubprotocolToken(r))
	}

	if valid {
		return true
	}

	// Mirrors requireAuthenticatedRead: a provider outage is 503, so a reconnecting tab
	// does not discard a session that may still be valid.
	if errors.Is(err, auth.ErrProviderUnavailable) {
		slog.Error("rejecting websocket: authentication provider unavailable", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, models.TaskStatus{
			Status: "authentication provider unavailable",
			Error:  err.Error(),
		})
		return false
	}

	if err != nil {
		slog.Warn("rejecting websocket with invalid credential", "error", err)
	} else {
		slog.Warn("rejecting unauthenticated websocket")
	}
	writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
		Status: unauthorizedMessage,
		Error:  "authentication required (offer the " + wsTokenSubprotocolPrefix + "<token> subprotocol)",
	})

	return false
}

func wsSubprotocolToken(request *http.Request) string {
	for _, offered := range strings.Split(request.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if token, found := strings.CutPrefix(strings.TrimSpace(offered), wsTokenSubprotocolPrefix); found {
			return token
		}
	}

	return ""
}

func (env *Env) handleWebSocketConnection(w http.ResponseWriter, r *http.Request) {
	// Before the upgrade, so a rejection is an ordinary HTTP response.
	if !env.authorizeWebSocket(w, r) {
		return
	}

	// Reject an upgrade the connection cannot carry (HTTP/2 has no hijack) with a
	// response, rather than letting websocket.Accept fail with the socket already
	// half-written.
	if _, ok := w.(http.Hijacker); !ok {
		slog.Error("ResponseWriter does not support hijacking")
		writeString(w, http.StatusInternalServerError, "WebSocket not supported")
		return
	}

	// Track the in-flight upgrade so graceful shutdown waits for handshakes that are
	// still in progress, not only for connections that are already established.
	// Bracketing the rest of the handler also gives Shutdown's connWg.Wait a
	// happens-before edge over the handshake's response writes; without it the only
	// synchronization between this handler and shutdown is the underlying TCP socket,
	// which the race detector cannot observe. Registered before the upgrade hijacks
	// the connection, which is the point net/http stops tracking the request itself.
	env.connWg.Add(1)
	defer env.connWg.Done()

	options := &websocket.AcceptOptions{
		InsecureSkipVerify: env.config.DevEnvironment, // dev only: skips the WebSocket origin/host check
		Subprotocols:       []string{wsSubprotocol},
	}

	conn, err := websocket.Accept(w, r, options)
	if err != nil {
		slog.Error("failed to accept websocket connection", "error", err)
		return
	}

	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()

	env.connWg.Add(1)
	go env.checkConnection(conn)
}

func (env *Env) checkConnection(c *websocket.Conn) {
	defer env.connWg.Done()

	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-env.shutdownCh:
			_ = c.Close(websocket.StatusGoingAway, "server shutdown")
			removeWebSocketConnection(c)
			return
		case <-ticker.C:
			// we are not using c.Ping here, because it's not working as expected
			// for some reason it's failing even if the connection is still alive
			// if you know how to fix it, please open an issue or PR
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if c.Write(ctx, websocket.MessageText, []byte("heartbeat")) != nil {
				cancel()
				_ = c.Close(websocket.StatusNormalClosure, "heartbeat failed")
				removeWebSocketConnection(c)
				return
			}
			cancel()
		}
	}
}

func notifyWebSocketClients(message string) {
	var wg sync.WaitGroup

	connectionsMutex.RLock()
	connsCopy := make([]*websocket.Conn, 0, len(connections))
	for _, c := range connections {
		if !closedConns[c] {
			connsCopy = append(connsCopy, c)
		}
	}
	connectionsMutex.RUnlock()

	for _, conn := range connsCopy {
		wg.Add(1)

		go func(c *websocket.Conn, message string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if c.Write(ctx, websocket.MessageText, []byte(message)) != nil {
				_ = c.Close(websocket.StatusNormalClosure, "write failed")
				removeWebSocketConnection(c)
			}
		}(conn, message)
	}

	wg.Wait()
}

// removeWebSocketConnection removes conn from the global connections slice under
// the mutex. Callers are responsible for closing the connection first.
func removeWebSocketConnection(conn *websocket.Conn) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	// Mark as closed first to prevent use-after-close during concurrent access
	closedConns[conn] = true

	for i := range connections {
		if connections[i] == conn {
			connections = append(connections[:i], connections[i+1:]...)
			break
		}
	}

	// Clean up closedConns entry to prevent memory leak
	delete(closedConns, conn)
}
