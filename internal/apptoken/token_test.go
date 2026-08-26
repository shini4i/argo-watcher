package apptoken

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeAllows(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		app   string
		want  bool
	}{
		{name: "wildcard covers any application", scope: Scope{AllApps: true}, app: "anything", want: true},
		{name: "wildcard covers an empty application name", scope: Scope{AllApps: true}, app: "", want: true},
		{name: "listed application is covered", scope: Scope{Apps: []string{"app1", "app2"}}, app: "app2", want: true},
		{name: "unlisted application is not covered", scope: Scope{Apps: []string{"app1"}}, app: "app2", want: false},
		{name: "empty scope covers nothing", scope: Scope{}, app: "app1", want: false},
		{name: "comparison is case sensitive", scope: Scope{Apps: []string{"app1"}}, app: "App1", want: false},
		{name: "a wildcard entry is not a wildcard", scope: Scope{Apps: []string{"*"}}, app: "app1", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.scope.Allows(test.app))
		})
	}
}

func TestScopeValidate(t *testing.T) {
	t.Run("normalizes an explicit list", func(t *testing.T) {
		scope := Scope{Apps: []string{" app1 ", "app2", "app1"}}

		require.NoError(t, scope.Validate())
		assert.Equal(t, []string{"app1", "app2"}, scope.Apps)
	})

	t.Run("clears the list of a wildcard token", func(t *testing.T) {
		scope := Scope{AllApps: true}

		require.NoError(t, scope.Validate())
		assert.Nil(t, scope.Apps)
	})

	t.Run("accepts the maximum list length", func(t *testing.T) {
		scope := Scope{Apps: distinctApps(MaxApps)}

		require.NoError(t, scope.Validate())
		assert.Len(t, scope.Apps, MaxApps)
	})

	tests := []struct {
		name  string
		scope Scope
		error string
	}{
		{
			name:  "rejects a wildcard that also lists applications",
			scope: Scope{AllApps: true, Apps: []string{"app1"}},
			error: "must not also list applications",
		},
		{
			name:  "rejects an empty scope",
			scope: Scope{},
			error: "at least one application or to all of them",
		},
		{
			name:  "rejects a scope that is only whitespace",
			scope: Scope{Apps: []string{"   "}},
			error: "must not be empty",
		},
		{
			name:  "rejects an oversized application name",
			scope: Scope{Apps: []string{strings.Repeat("a", MaxAppNameLength+1)}},
			error: "at most 255 characters",
		},
		{
			name:  "rejects a list longer than the cap",
			scope: Scope{Apps: distinctApps(MaxApps + 1)},
			error: "not list more than 200 applications",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.scope.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.error)
		})
	}
}

// distinctApps builds count distinct application names, so a test of the list cap
// exercises unique entries rather than reaching it with duplicates that
// normalization would have collapsed first.
func distinctApps(count int) []string {
	apps := make([]string, count)
	for i := range apps {
		apps[i] = fmt.Sprintf("app-%d", i)
	}
	return apps
}

func TestTokenUsable(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{name: "a nil token is unusable", token: nil, want: false},
		{name: "a live token without an expiry is usable", token: &Token{}, want: true},
		{name: "a token expiring later is usable", token: &Token{ExpiresAt: now.Add(time.Minute)}, want: true},
		{name: "a token expiring now is not usable", token: &Token{ExpiresAt: now}, want: false},
		{name: "an expired token is not usable", token: &Token{ExpiresAt: now.Add(-time.Second)}, want: false},
		{name: "a revoked token is not usable", token: &Token{RevokedAt: now.Add(-time.Hour)}, want: false},
		{
			name:  "revocation outranks an expiry in the future",
			token: &Token{ExpiresAt: now.Add(time.Hour), RevokedAt: now.Add(-time.Hour)},
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.token.Usable(now))
		})
	}
}

func TestNewCredential(t *testing.T) {
	credential, err := NewCredential()
	require.NoError(t, err)

	assert.True(t, HasPrefix(credential.Secret), "the secret must carry the routing prefix")
	assert.Len(t, credential.Secret, len(Prefix)+43, "32 random bytes render as 43 base64url characters")
	assert.Equal(t, Hash(credential.Secret), credential.Hash)
	assert.Equal(t, credential.Secret[len(credential.Secret)-hintLength:], credential.Hint)
	assert.NotContains(t, credential.Secret, "=", "the secret must be URL safe and unpadded")

	other, err := NewCredential()
	require.NoError(t, err)
	assert.NotEqual(t, credential.Secret, other.Secret)
}

func TestHasPrefix(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{token: "awt_abc", want: true},
		{token: Prefix, want: true},
		{token: "", want: false},
		{token: "eyJhbGciOiJIUzI1NiJ9.e30.sig", want: false},
		{token: "AWT_abc", want: false},
		{token: " awt_abc", want: false},
	}

	for _, test := range tests {
		t.Run(test.token, func(t *testing.T) {
			assert.Equal(t, test.want, HasPrefix(test.token))
		})
	}
}

func TestHashIsStable(t *testing.T) {
	assert.Equal(t, Hash("awt_example"), Hash("awt_example"))
	assert.NotEqual(t, Hash("awt_example"), Hash("awt_examplf"))
	assert.Len(t, Hash("awt_example"), 32)
}
