package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

// summaryEnv wires the handler to a repository whose GetAppSummaries is driven
// by the supplied function, and records the filter it was called with.
func summaryEnv(
	t *testing.T,
	respond func(models.TaskFilter) ([]models.AppSummary, error),
) (*chi.Mux, *models.TaskFilter) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockTaskRepository(ctrl)
	repo.EXPECT().Connect(gomock.Any()).Return(nil).AnyTimes()
	repo.EXPECT().ProcessObsoleteTasks(gomock.Any()).AnyTimes()

	seen := &models.TaskFilter{}
	repo.EXPECT().GetAppSummaries(gomock.Any()).
		DoAndReturn(func(filter models.TaskFilter) ([]models.AppSummary, error) {
			*seen = filter
			return respond(filter)
		}).AnyTimes()

	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))
	env := &Env{argo: argo, config: &config.ServerConfig{}}

	router := chi.NewRouter()
	router.Get("/api/v1/apps/summary", env.getAppSummaries)
	return router, seen
}

func getSummary(t *testing.T, router *chi.Mux, query string) (*httptest.ResponseRecorder, models.AppSummariesResponse) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, "/api/v1/apps/summary"+query, nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var body models.AppSummariesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, body
}

func TestGetAppSummariesReturnsAggregates(t *testing.T) {
	router, _ := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{
			{App: "checkout", Total: 4, Failed: 1, Running: 1, Deployed: 2, RecentStatuses: []string{"failed"}},
			{App: "payments", Total: 1, Deployed: 1, RecentStatuses: []string{"deployed"}},
		}, nil
	})

	w, body := getSummary(t, router, "?from_timestamp=0")

	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, body.Apps, 2)
	assert.Equal(t, 2, body.TotalApps)
	assert.Equal(t, int64(1), body.Apps[0].Failed)
	assert.Empty(t, body.Error)
}

func TestGetAppSummariesPassesTheWindowThrough(t *testing.T) {
	router, seen := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{}, nil
	})

	_, _ = getSummary(t, router, "?from_timestamp=1600000000&to_timestamp=1600003600")

	assert.Equal(t, float64(1600000000), seen.StartTime)
	assert.Equal(t, float64(1600003600), seen.EndTime)
}

func TestGetAppSummariesDefaultsTheUpperBoundToNow(t *testing.T) {
	router, seen := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{}, nil
	})

	before := float64(time.Now().Unix())
	_, _ = getSummary(t, router, "?from_timestamp=0")

	assert.GreaterOrEqual(t, seen.EndTime, before)
}

// The summary is a census of the window: a caller must not be able to narrow it
// to one app or status, which would make the counters mean something else.
func TestGetAppSummariesIgnoresAppAndStatusParams(t *testing.T) {
	router, seen := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{}, nil
	})

	_, _ = getSummary(t, router, "?from_timestamp=0&app=checkout&status=failed&search=x&author=y")

	assert.Empty(t, seen.App)
	assert.Empty(t, seen.Status)
	assert.Empty(t, seen.Search)
	assert.Empty(t, seen.Author)
}

func TestGetAppSummariesReportsABackendFailure(t *testing.T) {
	router, _ := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return nil, errors.New("failed to aggregate app summaries")
	})

	w, body := getSummary(t, router, "?from_timestamp=0")

	// A soft error in the body, matching how /api/v1/tasks reports one, so the
	// UI can show its retry state instead of a spinner that never resolves.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "failed to aggregate app summaries", body.Error)
	assert.Empty(t, body.Apps)
}

func TestGetAppSummariesTolerantOfAGarbageTimestamp(t *testing.T) {
	router, seen := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{}, nil
	})

	w, _ := getSummary(t, router, "?from_timestamp=not-a-number")

	// Mirrors /api/v1/tasks: an unparseable bound falls back rather than 400s —
	// here to the maximum look-back, never to the epoch.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.InDelta(t, maxAppSummaryWindow, seen.EndTime-seen.StartTime, 2)
}

func TestGetStateAuthorFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo, capture := newRepo(ctrl)
	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

	env := &Env{argo: argo, config: &config.ServerConfig{}}
	router := chi.NewRouter()
	router.Get("/api/v1/tasks", env.getState)

	get := func(t *testing.T, query string) *httptest.ResponseRecorder {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "/api/v1/tasks?from_timestamp=0&"+query, nil)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("reaches the filter, trimmed", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, get(t, "author=+jane%40example.com+").Code)
		assert.Equal(t, "jane@example.com", capture.lastFilter.Author)
	})

	// Author scopes to a person and search narrows within it; they are independent.
	t.Run("travels alongside search", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, get(t, "author=jane%40example.com&search=checkout").Code)
		assert.Equal(t, "jane@example.com", capture.lastFilter.Author)
		assert.Equal(t, "checkout", capture.lastFilter.Search)
	})

	t.Run("an absent param is a wildcard", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, get(t, "search=checkout").Code)
		assert.Empty(t, capture.lastFilter.Author)
	})

	t.Run("one rune past the field cap is rejected", func(t *testing.T) {
		atCap := strings.Repeat("é", models.MaxTaskFieldLength)
		assert.Equal(t, http.StatusOK, get(t, "author="+url.QueryEscape(atCap)).Code)

		w := get(t, "author="+url.QueryEscape(atCap+"é"))
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "author too long")
	})
}

// The queries carry no LIMIT and sort the whole window, so an unbounded
// look-back is a cheap way to make the database group every stored task.
func TestGetAppSummariesClampsTheLookBack(t *testing.T) {
	router, seen := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{}, nil
	})

	tests := []struct {
		name  string
		query string
	}{
		{name: "absent from_timestamp", query: ""},
		{name: "epoch from_timestamp", query: "?from_timestamp=0"},
		{name: "unparseable from_timestamp", query: "?from_timestamp=not-a-number"},
		{name: "negative from_timestamp", query: "?from_timestamp=-99999999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := getSummary(t, router, tt.query)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.InDelta(t, maxAppSummaryWindow, seen.EndTime-seen.StartTime, 2,
				"the window must be clamped to the maximum look-back")
		})
	}
}

func TestGetAppSummariesKeepsAWindowInsideTheCap(t *testing.T) {
	router, seen := summaryEnv(t, func(models.TaskFilter) ([]models.AppSummary, error) {
		return []models.AppSummary{}, nil
	})

	// 30 days, the widest the Web UI offers, must pass through untouched.
	end := time.Now().Unix()
	start := end - 30*24*60*60
	_, _ = getSummary(t, router, fmt.Sprintf("?from_timestamp=%d&to_timestamp=%d", start, end))

	assert.Equal(t, float64(start), seen.StartTime)
	assert.Equal(t, float64(end), seen.EndTime)
}
