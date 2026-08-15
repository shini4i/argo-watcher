package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

const (
	jsonContentType = "application/json; charset=utf-8"
	textContentType = "text/plain; charset=utf-8"
)

// payloadValidator enforces the `binding:"required"` struct tags on decoded request
// bodies. The tag name is the one the request models already carry, so the rejection
// messages clients receive stay unchanged.
var payloadValidator = func() *validator.Validate {
	v := validator.New()
	v.SetTagName("binding")
	return v
}()

// writeJSON renders payload as the response body with the given status code.
//
// The body is marshalled before anything is written so a marshalling failure can
// still produce a 500 rather than a truncated 200.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal response body", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Debug("failed to write response body", "error", err)
	}
}

func writeString(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", textContentType)
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		slog.Debug("failed to write response body", "error", err)
	}
}

// bindJSON decodes the request body into obj and validates its `binding` tags.
// It returns the decoding or validation error unchanged, because handlers surface
// it to the client as the reason the payload was rejected.
func bindJSON(r *http.Request, obj any) error {
	if r == nil || r.Body == nil {
		return errors.New("invalid request")
	}

	if err := json.NewDecoder(r.Body).Decode(obj); err != nil {
		return err
	}

	return payloadValidator.Struct(obj)
}

// contextKey is the private type of every value this package stores in a request
// context, so no other package can collide with these keys.
type contextKey string

// taskAppContextKey addresses the taskAppLabel holder placed by countUnauthenticatedRead.
const taskAppContextKey contextKey = "task_app"

// taskAppLabel carries the app name of the task resolved by getTaskStatus out to
// countUnauthenticatedRead, which records its metric after the handler returns.
// A pointer in the request context is what makes a value written by the handler
// visible to the middleware that already handed the request down.
type taskAppLabel struct {
	app string
}

func withTaskAppLabel(r *http.Request) (*http.Request, *taskAppLabel) {
	holder := &taskAppLabel{}
	return r.WithContext(context.WithValue(r.Context(), taskAppContextKey, holder)), holder
}

// setTaskApp records the app name of the task a handler resolved. It is a no-op
// when nothing is collecting the label, which is the case on every route that is
// not being counted.
func setTaskApp(r *http.Request, app string) {
	if holder, ok := r.Context().Value(taskAppContextKey).(*taskAppLabel); ok {
		holder.app = app
	}
}

var routeParam = regexp.MustCompile(`\{([^}]+)\}`)

// routePattern returns the matched route in the `/api/v1/tasks/:id` form.
//
// The value is a Prometheus label on the unauthenticated_reads counter that
// dashboards and alerts already select on, so the colon-prefixed spelling is a
// public contract; chi spells the same pattern with braces.
func routePattern(r *http.Request) string {
	routeCtx := chi.RouteContext(r.Context())
	if routeCtx == nil {
		return ""
	}

	return routeParam.ReplaceAllString(routeCtx.RoutePattern(), ":$1")
}
