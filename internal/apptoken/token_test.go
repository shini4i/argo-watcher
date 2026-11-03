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
		{
			name:  "a prefix covers an application starting with it",
			scope: Scope{AppPrefixes: []string{"whatever-"}},
			app:   "whatever-x7k2-review",
			want:  true,
		},
		{
			name:  "a prefix does not cover an application merely containing it",
			scope: Scope{AppPrefixes: []string{"whatever-"}},
			app:   "other-whatever-review",
			want:  false,
		},
		{
			name:  "a prefix does not cover the name without its separator",
			scope: Scope{AppPrefixes: []string{"whatever-"}},
			app:   "whateverprod",
			want:  false,
		},
		{
			name:  "a prefix is case sensitive",
			scope: Scope{AppPrefixes: []string{"whatever-"}},
			app:   "Whatever-review",
			want:  false,
		},
		{
			name:  "a scope mixing names and prefixes covers a listed name",
			scope: Scope{Apps: []string{"app1"}, AppPrefixes: []string{"whatever-"}},
			app:   "app1",
			want:  true,
		},
		{
			name:  "a scope mixing names and prefixes covers a prefixed name",
			scope: Scope{Apps: []string{"app1"}, AppPrefixes: []string{"whatever-"}},
			app:   "whatever-abc-review",
			want:  true,
		},
		{
			name:  "a scope mixing names and prefixes covers nothing else",
			scope: Scope{Apps: []string{"app1"}, AppPrefixes: []string{"whatever-"}},
			app:   "app2",
			want:  false,
		},
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

	t.Run("normalizes a prefix list", func(t *testing.T) {
		scope := Scope{AppPrefixes: []string{" whatever- ", "other-", "whatever-"}}

		require.NoError(t, scope.Validate())
		assert.Equal(t, []string{"whatever-", "other-"}, scope.AppPrefixes)
		assert.Nil(t, scope.Apps)
	})

	t.Run("accepts names and prefixes together", func(t *testing.T) {
		scope := Scope{Apps: []string{"app1"}, AppPrefixes: []string{"whatever-"}}

		require.NoError(t, scope.Validate())
		assert.Equal(t, []string{"app1"}, scope.Apps)
		assert.Equal(t, []string{"whatever-"}, scope.AppPrefixes)
	})

	t.Run("accepts a multi-segment prefix", func(t *testing.T) {
		scope := Scope{AppPrefixes: []string{"team-a-whatever-"}}

		require.NoError(t, scope.Validate())
	})

	t.Run("accepts the maximum combined length", func(t *testing.T) {
		scope := Scope{Apps: distinctApps(MaxApps - 1), AppPrefixes: []string{"whatever-"}}

		require.NoError(t, scope.Validate())
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
			name:  "rejects a wildcard that also lists prefixes",
			scope: Scope{AllApps: true, AppPrefixes: []string{"whatever-"}},
			error: "must not also list applications",
		},
		{
			name:  "rejects an empty scope",
			scope: Scope{},
			error: "at least one application or application prefix, or to all applications",
		},
		{
			name:  "rejects a glob written as an application name, pointing to app_prefixes",
			scope: Scope{Apps: []string{"whatever-*"}},
			error: `"whatever-*" is not an application name; list "whatever-" under app_prefixes`,
		},
		{
			name:  "rejects a glob whose stem is no valid prefix, without suggesting one",
			scope: Scope{Apps: []string{"whatever*"}},
			error: `"whatever*" is not an application name; list a prefix ending with "-" under app_prefixes`,
		},
		{
			name:  "rejects a leading glob without suggesting it",
			scope: Scope{Apps: []string{"*-review"}},
			error: `"*-review" is not an application name; list a prefix ending with "-" under app_prefixes`,
		},
		{
			name:  "rejects a prefix that is only whitespace",
			scope: Scope{AppPrefixes: []string{"  "}},
			error: "an application prefix must not be empty",
		},
		{
			name:  "rejects a prefix carrying a star",
			scope: Scope{AppPrefixes: []string{"whatever-*"}},
			error: `application prefix "whatever-*" must not contain "*"`,
		},
		{
			name:  "rejects a prefix not ending on a separator",
			scope: Scope{AppPrefixes: []string{"whatever"}},
			error: `application prefix "whatever" must end with "-"`,
		},
		{
			name:  "rejects a prefix that is only a separator",
			scope: Scope{AppPrefixes: []string{"-"}},
			error: `application prefix "-" must name at least one segment`,
		},
		{
			name:  "rejects a prefix made only of separators",
			scope: Scope{AppPrefixes: []string{"---"}},
			error: `application prefix "---" must name at least one segment`,
		},
		{
			name:  "rejects an oversized prefix",
			scope: Scope{AppPrefixes: []string{strings.Repeat("a", MaxAppNameLength) + "-"}},
			error: "an application prefix must be at most 255 characters",
		},
		{
			name:  "rejects names and prefixes together longer than the cap",
			scope: Scope{Apps: distinctApps(MaxApps), AppPrefixes: []string{"whatever-"}},
			error: "not list more than 200 applications and prefixes",
		},
		{
			name:  "rejects a scope that is only whitespace",
			scope: Scope{Apps: []string{"   "}},
			error: "must not be empty",
		},
		{
			name:  "rejects a star entry, pointing to all_apps",
			scope: Scope{Apps: []string{"app1", " * "}},
			error: `"*" is not an application name; use all_apps`,
		},
		{
			name:  "rejects all_apps written as an application name",
			scope: Scope{Apps: []string{"all_apps"}},
			error: `"all_apps" is not an application name; use all_apps`,
		},
		{
			name:  "rejects an oversized application name",
			scope: Scope{Apps: []string{strings.Repeat("a", MaxAppNameLength+1)}},
			error: "at most 255 characters",
		},
		{
			name:  "rejects a list longer than the cap",
			scope: Scope{Apps: distinctApps(MaxApps + 1)},
			error: "not list more than 200 applications and prefixes",
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

func TestScopeString(t *testing.T) {
	// This text reaches the operator in the rejection an out-of-scope token gets.
	assert.Equal(t, "all applications", Scope{AllApps: true}.String())
	assert.Equal(t, "app1, app2", Scope{Apps: []string{"app1", "app2"}}.String())
	assert.Equal(t, "app1", Scope{Apps: []string{"app1"}}.String())
	assert.Equal(t, "whatever-* (prefix)", Scope{AppPrefixes: []string{"whatever-"}}.String())
	assert.Equal(t, "app1, whatever-* (prefix), other-* (prefix)",
		Scope{Apps: []string{"app1"}, AppPrefixes: []string{"whatever-", "other-"}}.String())
	assert.Empty(t, Scope{}.String())
}

func TestValidatePrefix(t *testing.T) {
	// The JWT allowed_app_prefixes claim runs entries through this directly, without
	// the trimming Scope.Validate does first.
	require.NoError(t, ValidatePrefix("whatever-"))
	require.NoError(t, ValidatePrefix("a-"))
	require.ErrorContains(t, ValidatePrefix(""), "must not be empty")
	require.ErrorContains(t, ValidatePrefix("-"), "must name at least one segment")
	require.ErrorContains(t, ValidatePrefix("whatever"), `must end with "-"`)
	require.ErrorContains(t, ValidatePrefix("*-"), `must not contain "*"`)
	require.NoError(t, ValidatePrefix(strings.Repeat("a", MaxAppNameLength-1)+"-"))
	require.ErrorContains(t, ValidatePrefix(strings.Repeat("a", MaxAppNameLength)+"-"), "at most 255 characters")
}
