package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/shini4i/argo-watcher/internal/models"
)

type Token struct {
	Token string `json:"token"`
}

var (
	requestsCount = 0
)

func setupRouter() *chi.Mux {
	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/session", mockGenSession)
		r.Get("/session/userinfo", mockUserinfo)
		r.Get("/applications/{id}", mockReturnAppStatus)
	})

	return router
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal response body", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Debug("failed to write response body", "error", err)
	}
}

func mockGenSession(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Token{
		Token: "test_token",
	})
}

func mockUserinfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, models.Userinfo{LoggedIn: true})
}

func mockReturnAppStatus(w http.ResponseWriter, r *http.Request) {
	var appStatus models.Application

	apps := []string{"app", "app2", "app4"}

	app := chi.URLParam(r, "id")

	if !slices.Contains(apps, app) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		// Reproduces the message Argo CD returns, which the updater matches on. The
		// name is echoed into a text/plain body served only by this local test double.
		_, _ = fmt.Fprintf(w, "applications.argoproj.io \"%s\" not found", app) // #nosec G705 -- not HTML, and this server never runs outside a dev or e2e host
		return
	}

	if app == "app" {
		appStatus.Status.Sync.Status = "Synced"
	} else {
		appStatus.Status.Sync.Status = "OutOfSync"
	}

	appStatus.Status.Summary.Images = []string{"app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}

	if app == "app4" && requestsCount < 5 {
		slog.Info("app4 requests count", "count", requestsCount)
		requestsCount++
		if requestsCount < 2 {
			appStatus.Status.Summary.Images = []string{"app:v0.0.1-rc1", "nginx:1.21.6", "migrations:v0.0.1"}
		}
		appStatus.Status.Health.Status = "UhHealthy"
		slog.Info("app4 sync status", "status", appStatus.Status.Sync.Status)
		slog.Info("app4 health status", "status", appStatus.Status.Health.Status)
	} else if app == "app4" {
		requestsCount = 0
		appStatus.Status.Health.Status = "Healthy"
		appStatus.Status.Sync.Status = "Synced"
		slog.Info("app4 sync status", "status", appStatus.Status.Sync.Status)
		slog.Info("app4 health status", "status", appStatus.Status.Health.Status)
	} else {
		appStatus.Status.Health.Status = "Healthy"
	}

	writeJSON(w, http.StatusOK, appStatus)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("Starting mock web server")

	srv := &http.Server{
		Addr:              ":8081",
		Handler:           setupRouter(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("mock web server stopped", "error", err)
	}
}
