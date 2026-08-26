package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/apptoken"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

// fakeTokenStore is an in-process apptoken.Store for the handler tests. The
// production code ships no in-memory implementation on purpose, so the double
// lives here where losing it on restart does not matter.
type fakeTokenStore struct {
	tokens   []apptoken.Token
	used     []uuid.UUID
	secrets  map[string]uuid.UUID
	issueErr error
	listErr  error
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{secrets: map[string]uuid.UUID{}}
}

func (s *fakeTokenStore) Issue(scope apptoken.Scope, description, createdBy string, expiresAt time.Time) (*apptoken.IssuedToken, error) {
	if s.issueErr != nil {
		return nil, s.issueErr
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	credential, err := apptoken.NewCredential()
	if err != nil {
		return nil, err
	}

	token := apptoken.Token{
		Id:          uuid.New(),
		Scope:       scope,
		Hint:        credential.Hint,
		Description: description,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
	}
	s.tokens = append(s.tokens, token)
	s.secrets[credential.Secret] = token.Id

	return &apptoken.IssuedToken{Token: token, Secret: credential.Secret}, nil
}

func (s *fakeTokenStore) Lookup(secret string) (*apptoken.Token, error) {
	id, ok := s.secrets[secret]
	if !ok {
		return nil, apptoken.ErrNotFound
	}
	for index := range s.tokens {
		if s.tokens[index].Id == id {
			return &s.tokens[index], nil
		}
	}
	return nil, apptoken.ErrNotFound
}

func (s *fakeTokenStore) List() ([]apptoken.Token, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.tokens, nil
}

func (s *fakeTokenStore) Revoke(id uuid.UUID) error {
	for index := range s.tokens {
		if s.tokens[index].Id == id {
			if s.tokens[index].RevokedAt.IsZero() {
				s.tokens[index].RevokedAt = time.Now()
			}
			return nil
		}
	}
	return apptoken.ErrNotFound
}

func (s *fakeTokenStore) MarkUsed(id uuid.UUID) error {
	s.used = append(s.used, id)
	return nil
}

// namedOIDCStrategy is oidcLikeStrategy that can also name its user, which is how
// an issued token gets attributed to the operator who created it.
type namedOIDCStrategy struct {
	oidcLikeStrategy
	username string
}

func (s namedOIDCStrategy) Identify(string) (string, error) {
	return s.username, nil
}

// appTokenEnv builds a routed Env whose app token endpoints are registered, with
// the OIDC strategy standing in for the provider.
func appTokenEnv(t *testing.T, strategy auth.AuthStrategy, store apptoken.Store) *Env {
	t.Helper()

	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{oidcHeader: strategy})
	env.appTokens = store

	return env
}

// callAppTokens issues a request to the token endpoints carrying an OIDC session.
func callAppTokens(t *testing.T, env *Env, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, "/api/v1"+path, reader)
	require.NoError(t, err)
	req.Header.Set(oidcHeader, "Bearer oidc-token")
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	env.CreateRouter().ServeHTTP(recorder, req)

	return recorder
}

func privilegedStrategy(username string) auth.AuthStrategy {
	return namedOIDCStrategy{
		oidcLikeStrategy: oidcLikeStrategy{authenticated: true, privileged: true},
		username:         username,
	}
}

func TestIssueAppToken(t *testing.T) {
	t.Run("issues a scoped token and shows the secret once", func(t *testing.T) {
		store := newFakeTokenStore()
		env := appTokenEnv(t, privilegedStrategy("alice"), store)

		recorder := callAppTokens(t, env, http.MethodPost, appTokensEndpoint,
			`{"apps":["app1","app2"],"description":"ci pipeline"}`)

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())

		var issued models.AppTokenResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &issued))

		assert.True(t, apptoken.HasPrefix(issued.Secret), "the secret is returned to the operator once")
		assert.Equal(t, []string{"app1", "app2"}, issued.Apps)
		assert.False(t, issued.AllApps)
		assert.Equal(t, "alice", issued.CreatedBy, "the token is attributed to its issuer")
		assert.Equal(t, "ci pipeline", issued.Description)
		assert.NotEmpty(t, issued.Hint)
		assert.Zero(t, issued.ExpiresAt)

		// Without this a proxy or browser cache could replay the one copy of a secret
		// that can never be shown again.
		assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))

		// The list is the only other view of the token, and it must never carry it.
		listed := callAppTokens(t, env, http.MethodGet, appTokensEndpoint, "")
		require.Equal(t, http.StatusOK, listed.Code)
		assert.NotContains(t, listed.Body.String(), issued.Secret)
		assert.NotContains(t, listed.Body.String(), `"secret"`)
	})

	t.Run("issues a wildcard token", func(t *testing.T) {
		store := newFakeTokenStore()
		env := appTokenEnv(t, privilegedStrategy("alice"), store)

		recorder := callAppTokens(t, env, http.MethodPost, appTokensEndpoint, `{"all_apps":true}`)

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())

		var issued models.AppTokenResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &issued))
		assert.True(t, issued.AllApps)
		assert.Empty(t, issued.Apps)
	})

	t.Run("records the requested expiry", func(t *testing.T) {
		store := newFakeTokenStore()
		env := appTokenEnv(t, privilegedStrategy("alice"), store)

		recorder := callAppTokens(t, env, http.MethodPost, appTokensEndpoint,
			`{"apps":["app1"],"expires_in_days":30}`)

		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())

		var issued models.AppTokenResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &issued))
		assert.InDelta(t, time.Now().AddDate(0, 0, 30).UnixMilli(), issued.ExpiresAt, float64(time.Minute.Milliseconds()))
	})

	rejected := []struct {
		name string
		body string
	}{
		{name: "an empty scope", body: `{}`},
		{name: "a wildcard that also lists applications", body: `{"all_apps":true,"apps":["app1"]}`},
		{name: "a blank application name", body: `{"apps":["  "]}`},
		{name: "an expiry beyond the cap", body: `{"apps":["app1"],"expires_in_days":4000}`},
		{name: "a negative expiry", body: `{"apps":["app1"],"expires_in_days":-1}`},
		{name: "a malformed body", body: `{"apps":`},
		{name: "an over-long description", body: `{"apps":["app1"],"description":"` + strings.Repeat("d", 256) + `"}`},
		{name: "more applications than the cap", body: `{"apps":` + jsonAppList(201) + `}`},
	}

	for _, test := range rejected {
		t.Run("rejects "+test.name, func(t *testing.T) {
			store := newFakeTokenStore()
			env := appTokenEnv(t, privilegedStrategy("alice"), store)

			recorder := callAppTokens(t, env, http.MethodPost, appTokensEndpoint, test.body)

			assert.Equal(t, http.StatusNotAcceptable, recorder.Code, recorder.Body.String())
			assert.Empty(t, store.tokens, "a rejected request must not mint a token")
		})
	}
}

func TestAppTokenEndpointsRequirePrivilege(t *testing.T) {
	tests := []struct {
		name     string
		strategy auth.AuthStrategy
		status   int
	}{
		{
			name:     "an authenticated but unprivileged user",
			strategy: oidcLikeStrategy{authenticated: true},
			status:   http.StatusUnauthorized,
		},
		{
			name:     "an unauthenticated request",
			strategy: oidcLikeStrategy{},
			status:   http.StatusUnauthorized,
		},
		{
			name:     "a provider that cannot be consulted",
			strategy: oidcLikeStrategy{unavailable: true},
			status:   http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeTokenStore()
			env := appTokenEnv(t, test.strategy, store)

			issue := callAppTokens(t, env, http.MethodPost, appTokensEndpoint, `{"all_apps":true}`)
			assert.Equal(t, test.status, issue.Code)
			assert.Empty(t, store.tokens, "an unauthorized request must not mint a token")

			list := callAppTokens(t, env, http.MethodGet, appTokensEndpoint, "")
			assert.Equal(t, test.status, list.Code, "the list names who holds which credential")

			revoke := callAppTokens(t, env, http.MethodDelete, appTokensEndpoint+"/"+uuid.NewString(), "")
			assert.Equal(t, test.status, revoke.Code)
		})
	}
}

func TestRevokeAppToken(t *testing.T) {
	store := newFakeTokenStore()
	env := appTokenEnv(t, privilegedStrategy("alice"), store)

	issued, err := store.Issue(apptoken.Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)

	t.Run("revokes a token", func(t *testing.T) {
		recorder := callAppTokens(t, env, http.MethodDelete, appTokensEndpoint+"/"+issued.Id.String(), "")

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

		stored, err := store.Lookup(issued.Secret)
		require.NoError(t, err)
		assert.False(t, stored.Usable(time.Now()), "a revoked token must stop authorizing")
	})

	t.Run("reports an unknown token", func(t *testing.T) {
		recorder := callAppTokens(t, env, http.MethodDelete, appTokensEndpoint+"/"+uuid.NewString(), "")

		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("rejects an id that is not a UUID", func(t *testing.T) {
		recorder := callAppTokens(t, env, http.MethodDelete, appTokensEndpoint+"/not-a-uuid", "")

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})
}

func TestAppTokenEndpointsUnregisteredWithoutAStore(t *testing.T) {
	// Without a Postgres store the feature is off, and its endpoints must not exist
	// rather than answering 500 from a nil store.
	env := appTokenEnv(t, privilegedStrategy("alice"), nil)
	env.appTokens = nil

	for _, call := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, appTokensEndpoint},
		{http.MethodPost, appTokensEndpoint},
		{http.MethodDelete, appTokensEndpoint + "/" + uuid.NewString()},
	} {
		t.Run(call.method, func(t *testing.T) {
			recorder := callAppTokens(t, env, call.method, call.path, `{"all_apps":true}`)

			// The one discriminator that holds in both places: an unregistered route is
			// answered by the Web UI fallback, never by a handler. Here that is 404
			// text/plain because the static path is an empty temp dir; with a real bundle
			// it is 200 HTML. Either way it must not be the API, so a route that became
			// reachable (401/200/201 JSON) fails this.
			assert.NotContains(t, recorder.Header().Get("Content-Type"), "application/json",
				"the request must not have reached a handler")
			assert.Equal(t, http.StatusNotFound, recorder.Code)
		})
	}
}

func TestListAppTokensSurfacesAStoreFailure(t *testing.T) {
	store := newFakeTokenStore()
	store.listErr = errors.New("connection refused")
	env := appTokenEnv(t, privilegedStrategy("alice"), store)

	recorder := callAppTokens(t, env, http.MethodGet, appTokensEndpoint, "")

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "connection refused", "the internal reason stays in the log")
}

func TestAppTokenAttributionFallsBackToUnknown(t *testing.T) {
	store := newFakeTokenStore()
	// A provider that authorizes but names nobody: the action is still recorded.
	env := appTokenEnv(t, oidcLikeStrategy{authenticated: true, privileged: true}, store)

	recorder := callAppTokens(t, env, http.MethodPost, appTokensEndpoint, `{"apps":["app1"]}`)

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())

	var issued models.AppTokenResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &issued))
	assert.Equal(t, models.UnknownUser, issued.CreatedBy)
}

func TestAppTokenScopesTaskSubmission(t *testing.T) {
	store := newFakeTokenStore()
	scoped, err := store.Issue(apptoken.Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)

	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{
		oidcHeader:      privilegedStrategy("alice"),
		"Authorization": auth.NewPrefixRouter(auth.NewAppTokenAuthService(store), nil),
	})
	env.appTokens = store
	router := env.CreateRouter()

	submit := func(t *testing.T, app string) *httptest.ResponseRecorder {
		t.Helper()
		body := fmt.Sprintf(`{"app":%q,"author":"ci","project":"demo","images":[{"image":"nginx","tag":"1.0"}]}`, app)
		req, err := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+scoped.Secret)
		req.Header.Set("Content-Type", "application/json")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	// The task submission endpoint takes an optional credential, so a token outside
	// its scope is a rejection rather than a silently unvalidated task: accepting it
	// would skip the write-back and fail the rollout blaming the image.
	assert.Equal(t, http.StatusUnauthorized, submit(t, "app2").Code)
}

// TestAppTokenAuthorizesAnInScopeSubmission is the feature's central assertion: an
// in-scope token produces an accepted, *validated* task, which is what
// git_updater.go gates the write-back on. Without it, reverting addTask to the
// unscoped validateToken leaves the whole suite green — every app-token deployment
// would 401 and only the negative case would still pass.
func TestAppTokenAuthorizesAnInScopeSubmission(t *testing.T) {
	tests := []struct {
		name  string
		scope apptoken.Scope
		app   string
	}{
		{name: "a token scoped to the application", scope: apptoken.Scope{Apps: []string{"app1"}}, app: "app1"},
		{name: "a wildcard token on an unrelated application", scope: apptoken.Scope{AllApps: true}, app: "some-other-app"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeTokenStore()
			issued, err := store.Issue(test.scope, "", "alice", time.Time{})
			require.NoError(t, err)

			ctrl := gomock.NewController(t)
			repo := mocks.NewMockTaskRepository(ctrl)
			repo.EXPECT().Check().Return(true).AnyTimes()
			repo.EXPECT().CancelInProgressTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(int64(0), nil).AnyTimes()
			// AddTask consults the history for rollback detection before inserting.
			repo.EXPECT().GetTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]models.Task{}, int64(0)).AnyTimes()

			// Capture the task, then fail the insert: the success path would spawn the
			// real rollout goroutine. Validated is already decided by this point.
			var stored models.Task
			repo.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
				stored = task
				return nil, errors.New("stop before the rollout goroutine")
			})

			lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
			require.NoError(t, err)
			argo := &argocd.Argo{}
			argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

			strategies := map[string]auth.AuthStrategy{
				"Authorization": auth.NewPrefixRouter(auth.NewAppTokenAuthService(store), nil),
			}
			env := &Env{
				lockdown:      lockdown,
				strategies:    strategies,
				authenticator: auth.NewAuthenticator(strategies),
				argo:          argo,
				config:        &config.ServerConfig{DeploymentTimeout: 900},
			}

			router := chi.NewRouter()
			router.Post("/api/v1/tasks", env.addTask)

			body := fmt.Sprintf(
				`{"app":%q,"author":"ci","project":"demo","images":[{"image":"nginx","tag":"1.0"}]}`, test.app)
			req, err := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+issued.Secret)
			req.Header.Set("Content-Type", "application/json")

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			assert.True(t, stored.Validated, "an in-scope token must authorize the git write-back")
			assert.Equal(t, test.app, stored.App)
			assert.Equal(t, []uuid.UUID{issued.Id}, store.used, "an authorized deployment is recorded as a use")
		})
	}
}

// TestAppTokenDoesNotRecordAnUnauthorizedUse pairs with the above: the out-of-scope
// rejection must not look like a use of the token.
func TestAppTokenDoesNotRecordAnUnauthorizedUse(t *testing.T) {
	store := newFakeTokenStore()
	issued, err := store.Issue(apptoken.Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)

	strategies := map[string]auth.AuthStrategy{
		"Authorization": auth.NewPrefixRouter(auth.NewAppTokenAuthService(store), nil),
	}
	valid, err := auth.NewAuthenticator(strategies).ValidateForApp(
		requestWithToken(t, issued.Secret), "app2")

	assert.False(t, valid)
	require.Error(t, err)
	assert.Empty(t, store.used)
}

func requestWithToken(t *testing.T, secret string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/api/v1/tasks", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+secret)
	return req
}

// jsonAppList renders count distinct application names as a JSON array, for testing
// the list-length cap against unique entries rather than duplicates.
func jsonAppList(count int) string {
	apps := make([]string, count)
	for i := range apps {
		apps[i] = fmt.Sprintf("app-%d", i)
	}
	encoded, err := json.Marshal(apps)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
