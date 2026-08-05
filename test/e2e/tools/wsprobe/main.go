// Command wsprobe opens a single WebSocket connection to argo-watcher's /ws
// endpoint and streams what happens to it, one event per line on stdout:
//
//	OPEN                            the connection was established (printed once, immediately)
//	MSG <text>                      a text frame received (e.g. "heartbeat", "locked", "unlocked")
//	CLOSED code=<n> reason=<text>   the server closed the connection
//	DEADLINE                        DURATION elapsed with the connection still open
//	REJECTED status=<n>             the handshake was refused (e.g. 401 without a credential)
//
// A caller can wait for OPEN before acting (e.g. before triggering a shutdown),
// so the connection is guaranteed established rather than raced by a fixed sleep.
//
// It exists so two e2e phases can assert WebSocket behaviour without a bespoke
// client each:
//   - the lockdown phase greps the stream for `MSG locked` to prove the scheduled
//     lockdown watcher broadcast the transition;
//   - the shutdown-drain phase asserts the final `CLOSED code=1001 reason=server
//     shutdown` — proof the server drained the hijacked connection gracefully
//     (websocket.StatusGoingAway) rather than the socket dying abruptly.
//
// The process exits 0 once the connection closes (so a caller can `wait` on it)
// or once DURATION elapses. Output is flushed per line so a caller tailing the
// file sees events as they arrive.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// env returns the value of environment variable k, or def when it is unset or empty.
func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// dialOptions carries a credential when one is configured, which /ws requires once OIDC
// auth is enabled. The header variants send what any non-browser client would;
// WSPROBE_SUBPROTOCOL_TOKEN instead exercises the transport a browser is limited to.
//
// The WSPROBE_ prefix is load-bearing: the phase environment exports DEPLOY_TOKEN and
// JWT_SECRET suite-wide, so a bare name here would silently credential every probe —
// including the ones asserting that an uncredentialed handshake is refused.
func dialOptions() *websocket.DialOptions {
	opts := &websocket.DialOptions{HTTPHeader: http.Header{}}

	if token := os.Getenv("WSPROBE_OIDC_TOKEN"); token != "" {
		opts.HTTPHeader.Set("Oidc-Authorization", "Bearer "+token)
	}
	if token := os.Getenv("WSPROBE_DEPLOY_TOKEN"); token != "" {
		opts.HTTPHeader.Set("ARGO_WATCHER_DEPLOY_TOKEN", token)
	}
	if token := os.Getenv("WSPROBE_SUBPROTOCOL_TOKEN"); token != "" {
		opts.Subprotocols = []string{"argo-watcher.v1", "argo-watcher.token." + token}
	}

	return opts
}

func main() {
	wsURL := os.Getenv("WS_URL")
	if wsURL == "" {
		fmt.Fprintln(os.Stderr, "wsprobe: WS_URL is required")
		os.Exit(2)
	}
	dur, err := time.ParseDuration(env("DURATION", "60s"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wsprobe: invalid DURATION: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, dialOptions())
	if err != nil {
		// The status is what a phase asserting a rejected handshake needs, and the dial
		// error text alone does not carry it reliably.
		if resp != nil {
			fmt.Printf("REJECTED status=%d\n", resp.StatusCode)
		}
		fmt.Fprintf(os.Stderr, "wsprobe: dial failed: %v\n", err)
		os.Exit(1)
	}
	// The lab never sends frames larger than a heartbeat/lock notification.
	conn.SetReadLimit(4096)
	// Signal the connection is established so a caller can wait on it.
	fmt.Println("OPEN")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// DURATION elapsed with the connection still healthy: not a close.
			if ctx.Err() != nil {
				fmt.Println("DEADLINE")
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			// The server closed the connection: surface the close code + reason
			// so the drain phase can assert the graceful GoingAway/"server shutdown".
			code := int(websocket.CloseStatus(err))
			reason := ""
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				code = int(ce.Code)
				reason = ce.Reason
			}
			fmt.Printf("CLOSED code=%s reason=%s\n", strconv.Itoa(code), reason)
			return
		}
		fmt.Printf("MSG %s\n", string(data))
	}
}
