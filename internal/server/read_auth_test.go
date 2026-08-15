package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	promclient "github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/state"
)

// oidcLikeStrategy stands in for the OIDC auth service: it separates
// authentication from privileged authorization the same way, and can report a
// provider outage, which is what drives the 503 mapping.
type oidcLikeStrategy struct {
	authenticated bool
	privileged    bool
	unavailable   bool
}

func (s oidcLikeStrategy) Authenticate(string) error {
	if s.unavailable {
		return auth.ErrProviderUnavailable
	}
	if !s.authenticated {
		return errors.New("token validation failed with status: 401 Unauthorized")
	}
	return nil
}

func (s oidcLikeStrategy) Validate(string) (bool, error) {
	if err := s.Authenticate(""); err != nil {
		return false, err
	}
	if !s.privileged {
		return false, errors.New("someone is not a member of any of the privileged groups")
	}
	return true, nil
}

// readAuthEnv builds an Env with a full router, wiring the given strategies under
// the headers production uses. The metrics are real collectors on a private
// registry, so counter assertions read the value the server would actually export.
func readAuthEnv(t *testing.T, oidcEnabled bool, strategies map[string]auth.AuthStrategy) (*Env, *prometheus.Metrics) {
	t.Helper()
	return readAuthEnvWithTask(t, oidcEnabled, strategies, nil)
}

func readAuthEnvWithTask(t *testing.T, oidcEnabled bool, strategies map[string]auth.AuthStrategy, task *models.Task) (*Env, *prometheus.Metrics) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	// These tests assert routing and authentication, not task retrieval: a healthy
	// backend that knows no tasks keeps the handlers on their simplest path.
	repo.EXPECT().Check().Return(true).AnyTimes()
	if task != nil {
		repo.EXPECT().GetTask(gomock.Any()).Return(task, nil).AnyTimes()
	} else {
		repo.EXPECT().GetTask(gomock.Any()).Return(nil, state.ErrTaskNotFound).AnyTimes()
	}
	metrics := prometheus.NewMetrics(promclient.NewRegistry())
	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), metrics)

	lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)

	env := &Env{
		config: &config.ServerConfig{
			StaticFilePath: t.TempDir(),
			OIDC:           config.OIDCConfig{Enabled: oidcEnabled},
		},
		argo:          argo,
		metrics:       metrics,
		lockdown:      lockdown,
		strategies:    strategies,
		authenticator: auth.NewAuthenticator(strategies),
	}

	return env, metrics
}

func unauthenticatedReads(metrics *prometheus.Metrics, path, app string) float64 {
	return promtestutil.ToFloat64(metrics.UnauthenticatedReads.WithLabelValues(path, app))
}

func getWith(t *testing.T, env *Env, path, header, value string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, http.NoBody)
	require.NoError(t, err)
	if header != "" {
		req.Header.Set(header, value)
	}

	recorder := httptest.NewRecorder()
	env.CreateRouter().ServeHTTP(recorder, req)
	return recorder
}

var protectedReads = []string{
	"/api/v1/tasks?from_timestamp=0",
	"/api/v1/version",
	"/api/v1/deploy-lock",
	"/api/v1/reachability",
}

// TestReadAuthDisabled pins that the OIDC-disabled deployment is untouched: with
// no auth backend configured there is nothing to authenticate against, so every
// read stays open exactly as before.
func TestReadAuthDisabled(t *testing.T) {
	env, _ := readAuthEnv(t, false, nil)

	for _, path := range append([]string{"/api/v1/config"}, protectedReads...) {
		t.Run(path, func(t *testing.T) {
			assert.Equal(t, http.StatusOK, getWith(t, env, path, "", "").Code)
		})
	}
}

// TestReadAuthProtectedEndpoints covers the endpoints that require a credential
// once OIDC is enabled. These are the browser-facing reads: no released client
// polls them, so requiring auth here breaks no pipeline.
func TestReadAuthProtectedEndpoints(t *testing.T) {
	t.Run("rejects a request carrying no credential", func(t *testing.T) {
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: true},
		})

		for _, path := range protectedReads {
			recorder := getWith(t, env, path, "", "")
			assert.Equal(t, http.StatusUnauthorized, recorder.Code, path)
			assert.Contains(t, recorder.Body.String(), "authentication required", path)
		}
	})

	t.Run("accepts an authenticated user who holds no privilege", func(t *testing.T) {
		// The split matters here: read access must not be limited to
		// OIDC_PRIVILEGED_GROUPS, or enabling this would lock most users out.
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: true, privileged: false},
		})

		for _, path := range protectedReads {
			assert.Equal(t, http.StatusOK, getWith(t, env, path, oidcHeader, "Bearer token").Code, path)
		}
	})

	t.Run("accepts the legacy Keycloak header", func(t *testing.T) {
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader:           oidcLikeStrategy{authenticated: true},
			legacyKeycloakHeader: oidcLikeStrategy{authenticated: true},
		})

		assert.Equal(t, http.StatusOK,
			getWith(t, env, "/api/v1/tasks?from_timestamp=0", legacyKeycloakHeader, "Bearer token").Code)
	})

	t.Run("accepts a deploy token so a pipeline can read", func(t *testing.T) {
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader:                  oidcLikeStrategy{authenticated: true},
			"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService("deploy-token"),
		})

		assert.Equal(t, http.StatusOK,
			getWith(t, env, "/api/v1/tasks?from_timestamp=0", "ARGO_WATCHER_DEPLOY_TOKEN", "deploy-token").Code)
	})

	t.Run("rejects a credential the provider refused", func(t *testing.T) {
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: false},
		})

		recorder := getWith(t, env, "/api/v1/tasks?from_timestamp=0", oidcHeader, "Bearer token")

		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "401 Unauthorized")
	})

	t.Run("reports an unreachable provider as unavailable, not unauthorized", func(t *testing.T) {
		// A 401 makes the frontend drop its session and redirect to login, so a
		// provider blip must never be reported as an auth failure.
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{unavailable: true},
		})

		recorder := getWith(t, env, "/api/v1/tasks?from_timestamp=0", oidcHeader, "Bearer token")

		assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	})

	t.Run("still reports unavailable when a second credential is rejected", func(t *testing.T) {
		// The production shape of an outage: the Web UI sends its OIDC token in both
		// headers, and with JWT_SECRET configured the Authorization copy is parsed as
		// an HMAC JWT and rejected. Strategy order is randomized, so answering 401
		// here would sign users out of valid sessions on roughly half of requests.
		env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader:      oidcLikeStrategy{unavailable: true},
			"Authorization": auth.NewJWTAuthService("secret"),
		})
		router := env.CreateRouter()

		for i := 0; i < 50; i++ {
			req, err := http.NewRequest(http.MethodGet, "/api/v1/tasks?from_timestamp=0", http.NoBody)
			require.NoError(t, err)
			req.Header.Set(oidcHeader, "Bearer oidc-token")
			req.Header.Set("Authorization", "Bearer not-an-hmac-jwt")

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, "iteration %d", i)
		}
	})
}

// TestDeployLockProviderUnavailable pins the same 401-vs-503 distinction on the
// privileged write path: clicking the lock button during a provider outage must
// surface an outage, not sign the user out.
func TestDeployLockProviderUnavailable(t *testing.T) {
	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{unavailable: true},
	})

	req, err := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", http.NoBody)
	require.NoError(t, err)
	req.Header.Set(oidcHeader, "Bearer token")

	recorder := httptest.NewRecorder()
	env.CreateRouter().ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.False(t, env.lockdown.IsLocked(), "a failed authorization must not set the lock")
}

func TestReadAuthOpenEndpoints(t *testing.T) {
	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{authenticated: true},
	})

	t.Run("config stays open because login bootstraps from it", func(t *testing.T) {
		// The frontend must read issuer_url and client_id before it can obtain a
		// token, so gating this endpoint would make login impossible.
		assert.Equal(t, http.StatusOK, getWith(t, env, "/api/v1/config", "", "").Code)
	})

	t.Run("probe endpoints stay open", func(t *testing.T) {
		// A kubelet cannot perform an OIDC flow, so gating these would make every
		// pod fail its probes the moment OIDC is enabled.
		for _, path := range []string{"/livez", "/readyz"} {
			assert.Equal(t, http.StatusOK, getWith(t, env, path, "", "").Code, path)
		}
	})

	t.Run("metrics stays open for scraping", func(t *testing.T) {
		// Prometheus cannot perform an OIDC flow; this endpoint is governed by
		// network policy instead.
		assert.Equal(t, http.StatusOK, getWith(t, env, "/metrics", "", "").Code)
	})

	t.Run("task submission stays open without a credential", func(t *testing.T) {
		// The highest-blast-radius invariant in this change: moving POST /tasks into
		// the authenticated group would reject every uncredentialed pipeline in the
		// fleet at once. A malformed body is enough to prove the request reached the
		// handler — 406 comes from payload parsing, 401 would come from the middleware.
		req, err := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader("{"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		recorder := httptest.NewRecorder()
		env.CreateRouter().ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusNotAcceptable, recorder.Code)
		assert.NotEqual(t, http.StatusUnauthorized, recorder.Code)
	})
}

// TestReadAuthCoversEveryRegisteredRead derives its expectations from the router's own
// route table rather than a hand-kept list, so a read added later to the wrong group
// fails here without anyone remembering to extend a test.
func TestReadAuthCoversEveryRegisteredRead(t *testing.T) {
	openByDesign := map[string]bool{
		"/api/v1/config":     true,
		"/api/v1/tasks/{id}": true,
	}

	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{authenticated: true},
	})
	router := env.CreateRouter()

	checked := 0
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet || !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}

		// Route patterns carry parameter placeholders; any concrete value reaches the
		// same handler chain, which is all this assertion cares about.
		path := strings.ReplaceAll(route, "{id}", "00000000-0000-0000-0000-000000000000")

		req, err := http.NewRequest(method, path, http.NoBody)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		checked++
		if openByDesign[route] {
			assert.NotEqual(t, http.StatusUnauthorized, recorder.Code,
				"%s is exempt by design and must stay reachable", route)
			return nil
		}

		assert.Equal(t, http.StatusUnauthorized, recorder.Code,
			"%s is a read with no documented exemption and must require a credential", route)
		return nil
	})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, checked, 6, "the route table should have yielded every /api/v1 read")
}

// TestReadAuthTaskLookupRemainsOpen pins the deliberate exemption: released clients
// poll this lookup without a credential, so gating it would break every pipeline. The
// v4 UUID is the capability, and the enumerable list endpoint is protected.
func TestReadAuthTaskLookupRemainsOpen(t *testing.T) {
	const (
		unknownTask = "/api/v1/tasks/00000000-0000-0000-0000-000000000000"
		routePath   = "/api/v1/tasks/:id" // the metric label, unchanged since it is what dashboards select on
	)

	oidcStrategies := func() map[string]auth.AuthStrategy {
		return map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: true},
		}
	}

	t.Run("serves a lookup without a credential", func(t *testing.T) {
		env, _ := readAuthEnv(t, true, oidcStrategies())

		// 404 proves the request reached the handler rather than being rejected.
		assert.Equal(t, http.StatusNotFound, getWith(t, env, unknownTask, "", "").Code)
	})

	t.Run("counts the uncredentialed read for the migration signal", func(t *testing.T) {
		env, metrics := readAuthEnv(t, true, oidcStrategies())

		getWith(t, env, unknownTask, "", "")

		assert.Equal(t, float64(1), unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})

	t.Run("labels the count with the application behind the task", func(t *testing.T) {
		// Without this label the counter says a migration is unfinished and nothing
		// about whose pipeline to go and upgrade.
		env, metrics := readAuthEnvWithTask(t, true, oidcStrategies(),
			&models.Task{Id: "task-id", App: "payments-api", Validated: true})

		getWith(t, env, unknownTask, "", "")

		assert.Equal(t, float64(1), unauthenticatedReads(metrics, routePath, "payments-api"))
		assert.Zero(t, unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})

	t.Run("does not label an app supplied by an uncredentialed submission", func(t *testing.T) {
		// Otherwise an open POST + open lookup mints a permanent series per request.
		env, metrics := readAuthEnvWithTask(t, true, oidcStrategies(),
			&models.Task{Id: "task-id", App: "attacker-controlled-name", Validated: false})

		getWith(t, env, unknownTask, "", "")

		assert.Equal(t, float64(1), unauthenticatedReads(metrics, routePath, models.UnknownApp))
		assert.Zero(t, unauthenticatedReads(metrics, routePath, "attacker-controlled-name"))
	})

	t.Run("does not count a credentialed read", func(t *testing.T) {
		env, metrics := readAuthEnv(t, true, oidcStrategies())

		getWith(t, env, unknownTask, oidcHeader, "Bearer token")

		assert.Zero(t, unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})

	t.Run("does not count a read whose credential was rejected", func(t *testing.T) {
		// The counter measures un-migrated callers, so an expired or wrong token must
		// not inflate it — that number falling to zero is what licenses closing this
		// endpoint, and a caller sending a bad token has already migrated.
		env, metrics := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: false},
		})

		getWith(t, env, unknownTask, oidcHeader, "Bearer expired-token")

		assert.Zero(t, unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})

	t.Run("does not count a credentialed read during a provider outage", func(t *testing.T) {
		// The discriminating case for counting on presence rather than validity: a
		// caller that HAS migrated must not be recorded as un-migrated just because
		// the provider happens to be unreachable, and the poll — which runs for the
		// whole duration of every deployment — must not wait on the provider to find
		// that out.
		env, metrics := readAuthEnv(t, true, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{unavailable: true},
		})

		assert.Equal(t, http.StatusNotFound, getWith(t, env, unknownTask, oidcHeader, "Bearer token").Code)
		assert.Zero(t, unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})

	t.Run("does not count anything when OIDC is disabled", func(t *testing.T) {
		// Without an auth backend there is no migration to measure.
		env, metrics := readAuthEnv(t, false, nil)

		getWith(t, env, unknownTask, "", "")

		assert.Zero(t, unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})
}

// TestReadAuthTaskLookupEnforced covers the end of the migration: with
// OIDC_REQUIRE_TASK_READ_AUTH set, the lookup the client polls is gated like every
// other read. Nothing else about the endpoint changes — the same credentials are
// accepted and a provider outage is still a 503, not a 401.
func TestReadAuthTaskLookupEnforced(t *testing.T) {
	const (
		unknownTask = "/api/v1/tasks/00000000-0000-0000-0000-000000000000"
		routePath   = "/api/v1/tasks/:id" // the metric label, unchanged since it is what dashboards select on
	)

	enforcedEnv := func(t *testing.T, strategies map[string]auth.AuthStrategy) (*Env, *prometheus.Metrics) {
		t.Helper()
		env, metrics := readAuthEnv(t, true, strategies)
		env.config.OIDC.RequireTaskReadAuth = true
		return env, metrics
	}

	t.Run("rejects a lookup without a credential", func(t *testing.T) {
		env, _ := enforcedEnv(t, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: true},
		})

		assert.Equal(t, http.StatusUnauthorized, getWith(t, env, unknownTask, "", "").Code)
	})

	t.Run("serves a lookup carrying a credential", func(t *testing.T) {
		env, _ := enforcedEnv(t, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: true},
		})

		// 404 proves the request reached the handler rather than being rejected.
		assert.Equal(t, http.StatusNotFound, getWith(t, env, unknownTask, oidcHeader, "Bearer token").Code)
	})

	t.Run("accepts a deploy token, so pipelines keep polling", func(t *testing.T) {
		const (
			deployToken       = "s3cr3t-deploy-token"
			deployTokenHeader = "ARGO_WATCHER_DEPLOY_TOKEN"
		)
		env, _ := enforcedEnv(t, map[string]auth.AuthStrategy{
			deployTokenHeader: auth.NewDeployTokenAuthService(deployToken),
		})

		assert.Equal(t, http.StatusNotFound, getWith(t, env, unknownTask, deployTokenHeader, deployToken).Code)
	})

	t.Run("reports a provider outage as 503, never 401", func(t *testing.T) {
		// The client retries a 5xx and gives up on a 4xx, so mapping an outage to 401
		// would fail deployments that a brief blip should have survived.
		env, _ := enforcedEnv(t, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{unavailable: true},
		})

		assert.Equal(t, http.StatusServiceUnavailable, getWith(t, env, unknownTask, oidcHeader, "Bearer token").Code)
	})

	t.Run("stops counting once the endpoint is closed", func(t *testing.T) {
		// The counter exists to license this switch; with it on there is no
		// unauthenticated read left to count, so the series must stay flat.
		env, metrics := enforcedEnv(t, map[string]auth.AuthStrategy{
			oidcHeader: oidcLikeStrategy{authenticated: true},
		})

		getWith(t, env, unknownTask, "", "")

		assert.Zero(t, unauthenticatedReads(metrics, routePath, models.UnknownApp))
	})
}
