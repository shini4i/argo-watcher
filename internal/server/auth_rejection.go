package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
)

// Hints naming how to present a credential. A browser cannot set a header on a
// WebSocket handshake, so the socket names its subprotocol instead.
const (
	headerCredentialHint = "set " + oidcHeader + " header"
	wsCredentialHint     = "offer the " + wsTokenSubprotocolPrefix + "<token> subprotocol"
)

// writeAuthRejection writes the response for a credential that was not accepted:
// 503 for ErrProviderUnavailable, since the Web UI discards its session on a 401,
// and 401 otherwise. subject names the rejected thing in the log; credentialHint
// tells a caller that sent nothing how to present one.
func writeAuthRejection(w http.ResponseWriter, r *http.Request, err error, subject, credentialHint string) {
	if errors.Is(err, auth.ErrProviderUnavailable) {
		slog.Error("rejecting "+subject+": authentication provider unavailable",
			"method", r.Method, "url", r.URL.Path, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, models.TaskStatus{
			Status: providerUnavailableMessage,
			Error:  err.Error(),
		})
		return
	}

	// The strategy's own reason, so the client can show something actionable.
	if err != nil {
		slog.Warn("rejecting "+subject+" with invalid credential",
			"method", r.Method, "url", r.URL.Path, "error", err)
		writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  err.Error(),
		})
		return
	}

	slog.Warn("rejecting unauthenticated "+subject, "method", r.Method, "url", r.URL.Path)
	writeJSON(w, http.StatusUnauthorized, models.TaskStatus{
		Status: unauthorizedMessage,
		Error:  "authentication required (" + credentialHint + ")",
	})
}
