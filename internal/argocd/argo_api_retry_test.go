package argocd

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client's jar appends the ArgoCD session cookie to the request it is given,
// so retrying with the same *http.Request accumulates one copy of the token per
// attempt. A proxy that rejects the oversized header answers 4xx, which is not
// treated as an outage, so the deployment is recorded as failed rather than aborted.
func TestArgoApiDoGetSendsTheTokenOncePerAttempt(t *testing.T) {
	baseURL, err := url.Parse("https://example.com")
	require.NoError(t, err)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	jar.SetCookies(baseURL, []*http.Cookie{{Name: "argocd.token", Value: "secret"}})

	var cookieHeaders []string
	api := NewArgoApi()
	api.baseUrl = *baseURL
	api.maxRetries = 2
	api.client = &http.Client{
		Jar: jar,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			cookieHeaders = append(cookieHeaders, req.Header.Get("Cookie"))
			return nil, errors.New("connection refused")
		}),
	}

	_, _, err = api.doGet(context.Background(), "https://example.com/api/v1/session/userinfo")
	require.Error(t, err)
	require.Len(t, cookieHeaders, 2, "every attempt must reach the transport")

	for attempt, header := range cookieHeaders {
		assert.Equal(t, 1, strings.Count(header, "argocd.token="),
			"attempt %d must carry the token exactly once, got %q", attempt+1, header)
	}
}

// Every attempt is a fresh clone of one request, so a header the caller set must
// survive to the last of them.
func TestArgoApiDoGetKeepsItsHeadersAcrossRetries(t *testing.T) {
	baseURL, err := url.Parse("https://example.com")
	require.NoError(t, err)

	var accepts []string
	api := NewArgoApi()
	api.baseUrl = *baseURL
	api.maxRetries = 2
	api.client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			accepts = append(accepts, req.Header.Get("Accept"))
			return nil, errors.New("connection refused")
		}),
	}

	_, _, err = api.doGet(context.Background(), "https://example.com/api/v1/session/userinfo")
	require.Error(t, err)
	require.Len(t, accepts, 2)
	for _, accept := range accepts {
		assert.Equal(t, "application/json", accept)
	}
}

// ctxKey types the sentinel the clone must carry through.
type ctxKey struct{}

// Clone(ctx) is the only thing attaching the caller's context to the in-flight
// round-trip now that the request is rebuilt per attempt. Cloning from
// context.Background() instead would leave a call bounded only by the client
// timeout, well past the rollout deadline doGet promises to honour.
func TestArgoApiDoGetClonesWithTheCallersContext(t *testing.T) {
	baseURL, err := url.Parse("https://example.com")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "sentinel"))
	defer cancel()

	var seen context.Context
	api := NewArgoApi()
	api.baseUrl = *baseURL
	api.maxRetries = 1
	api.client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			seen = req.Context()
			return nil, errors.New("connection refused")
		}),
	}

	_, _, err = api.doGet(ctx, "https://example.com/api/v1/session/userinfo")
	require.Error(t, err)

	require.NotNil(t, seen)
	assert.Equal(t, "sentinel", seen.Value(ctxKey{}), "the request must carry the caller's context, not a fresh one")

	cancel()
	assert.ErrorIs(t, seen.Err(), context.Canceled, "cancelling the caller must reach the in-flight request")
}
