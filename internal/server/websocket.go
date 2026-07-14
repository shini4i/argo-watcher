package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

var (
	connectionsMutex sync.RWMutex
	connections      []*websocket.Conn
	closedConns      = make(map[*websocket.Conn]bool) // Track closed connections to prevent use-after-close
)

// wsResponseWriter wraps gin's ResponseWriter to provide proper WebSocket hijacking.
// gin's ResponseWriter fails Hijack() after WriteHeader() is called, but WebSocket
// upgrade requires both operations. This wrapper hijacks the connection early
// (before WriteHeader) and stores the raw connection for later use.
type wsResponseWriter struct {
	gin.ResponseWriter
	conn          net.Conn
	brw           *bufio.ReadWriter
	headerWritten bool
}

// Hijack returns the pre-hijacked connection, bypassing gin's "already written" check.
func (w *wsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.conn == nil {
		return nil, nil, errors.New("connection was not pre-hijacked")
	}
	return w.conn, w.brw, nil
}

// Write writes data through the buffered writer to maintain consistency.
func (w *wsResponseWriter) Write(data []byte) (int, error) {
	if w.brw == nil {
		return 0, errors.New("buffered writer not available")
	}
	n, err := w.brw.Write(data)
	if err != nil {
		return n, err
	}
	return n, w.brw.Flush()
}

// WriteHeader writes the status line and headers through the buffered writer.
// Note: The http.ResponseWriter interface does not allow WriteHeader to return an error,
// so errors are logged but cannot be propagated to the caller.
// Per http.ResponseWriter contract, multiple calls should be no-ops after the first.
func (w *wsResponseWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.headerWritten = true

	if w.brw == nil {
		slog.Error("buffered writer not available during WriteHeader")
		return
	}
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", code, http.StatusText(code))
	if _, err := w.brw.WriteString(statusLine); err != nil {
		slog.Error("failed to write status line during WebSocket upgrade", "error", err)
		return
	}
	if err := w.Header().Write(w.brw); err != nil {
		slog.Error("failed to write headers during WebSocket upgrade", "error", err)
		return
	}
	if _, err := w.brw.WriteString("\r\n"); err != nil {
		slog.Error("failed to write header terminator during WebSocket upgrade", "error", err)
		return
	}
	if err := w.brw.Flush(); err != nil {
		slog.Error("failed to flush WebSocket upgrade response", "error", err)
	}
}

// handleWebSocketConnection accepts a WebSocket connection, adds it to a slice,
// and initiates a goroutine to ping the connection regularly. If WebSocket
// acceptance fails, an error is logged. The goroutine serves to monitor
// the connection's activity and removes it from the slice if it's inactive.
func (env *Env) handleWebSocketConnection(c *gin.Context) {
	options := &websocket.AcceptOptions{
		InsecureSkipVerify: env.config.DevEnvironment, // It will disable websocket host validation if set to true
	}

	// Pre-hijack the connection BEFORE WriteHeader is called
	// gin's ResponseWriter fails Hijack after WriteHeader, so we hijack first
	hijacker, ok := c.Writer.(http.Hijacker)
	if !ok {
		slog.Error("ResponseWriter does not support hijacking")
		c.String(http.StatusInternalServerError, "WebSocket not supported")
		return
	}

	netConn, brw, err := hijacker.Hijack()
	if err != nil {
		slog.Error("failed to hijack connection for WebSocket", "error", err)
		// After a failed hijack, the connection state is unknown and we cannot reliably
		// write a response. The client connection will eventually timeout.
		return
	}

	// Create wrapper with pre-hijacked connection
	wrappedWriter := &wsResponseWriter{
		ResponseWriter: c.Writer,
		conn:           netConn,
		brw:            brw,
	}

	conn, err := websocket.Accept(wrappedWriter, c.Request, options)
	if err != nil {
		slog.Error("failed to accept websocket connection", "error", err)
		_ = netConn.Close() // #nosec G104 - best effort cleanup, already in error path
		return
	}

	// Append the connection before starting the goroutine
	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()

	// Track the goroutine for graceful shutdown
	env.connWg.Add(1)
	go env.checkConnection(conn)
}

// checkConnection is a method for the Env struct that continuously checks the
// health of a WebSocket connection by sending periodic "heartbeat" messages.
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

// notifyWebSocketClients is a function that sends a message to all active WebSocket clients.
// It iterates over the global connections slice, which contains all active WebSocket connections,
// and sends the provided message to each connection using the wsjson.Write function.
// If an error occurs while sending the message to a connection (for example, if the connection has been closed),
// it removes the connection from the connections slice to prevent further attempts to send messages to it.
func notifyWebSocketClients(message string) {
	var wg sync.WaitGroup

	// Copy connections slice under mutex to avoid race condition during iteration
	// Also filter out closed connections
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

// removeWebSocketConnection is a helper function that removes a WebSocket connection
// from the global connections slice. It is used to clean up connections that are no longer active.
// The function takes a WebSocket connection as an argument and removes it from the connections slice.
// It uses a mutex to prevent concurrent access to the connections slice, ensuring thread safety.
// Note: Callers are responsible for closing the connection before calling this function.
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
