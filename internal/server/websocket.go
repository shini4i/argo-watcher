package server

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// wsRegistry holds the WebSocket connections one server broadcasts to. It is a
// field of Env rather than a package variable so that two servers running in the
// same process — which is every test that starts one — never see each other's
// clients.
type wsRegistry struct {
	mu    sync.RWMutex
	conns []*websocket.Conn
}

func (r *wsRegistry) add(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.conns = append(r.conns, conn)
}

// remove drops conn from the registry. Callers close the connection first.
// slices.Delete clears the vacated tail slot, so a closed connection is not left
// reachable through the backing array.
func (r *wsRegistry) remove(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if i := slices.Index(r.conns, conn); i >= 0 {
		r.conns = slices.Delete(r.conns, i, i+1)
	}
}

// snapshot copies the registered connections, so a broadcast can write to them
// without holding the lock for the length of every write.
func (r *wsRegistry) snapshot() []*websocket.Conn {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return slices.Clone(r.conns)
}

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

	writeAuthRejection(w, r, err, "websocket", wsCredentialHint)
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

	env.ws.add(conn)

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
			env.ws.remove(c)
			return
		case <-ticker.C:
			// we are not using c.Ping here, because it's not working as expected
			// for some reason it's failing even if the connection is still alive
			// if you know how to fix it, please open an issue or PR
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if c.Write(ctx, websocket.MessageText, []byte("heartbeat")) != nil {
				cancel()
				_ = c.Close(websocket.StatusNormalClosure, "heartbeat failed")
				env.ws.remove(c)
				return
			}
			cancel()
		}
	}
}

// notifyWebSocketClients pushes message to every connected client, dropping any
// connection the write fails on. It returns once every write has finished or
// timed out.
func (env *Env) notifyWebSocketClients(message string) {
	var wg sync.WaitGroup

	for _, conn := range env.ws.snapshot() {
		wg.Add(1)

		go func(c *websocket.Conn) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if c.Write(ctx, websocket.MessageText, []byte(message)) != nil {
				_ = c.Close(websocket.StatusNormalClosure, "write failed")
				env.ws.remove(c)
			}
		}(conn)
	}

	wg.Wait()
}
