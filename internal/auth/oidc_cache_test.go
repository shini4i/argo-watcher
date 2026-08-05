package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signedTokenWithExp builds an HMAC-signed JWT carrying the given expiry. The
// signature is irrelevant — tokenExpiry only decodes the payload — but signing keeps
// the fixture a realistic token.
func signedTokenWithExp(t *testing.T, exp time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": exp.Unix(),
	})
	signed, err := token.SignedString([]byte("irrelevant"))
	require.NoError(t, err)
	return signed
}

func TestTokenExpiry(t *testing.T) {
	t.Run("reads the exp claim from a JWT access token", func(t *testing.T) {
		want := time.Now().Add(7 * time.Minute).Truncate(time.Second)

		got, ok := tokenExpiry(signedTokenWithExp(t, want))

		assert.True(t, ok)
		assert.WithinDuration(t, want, got, time.Second)
	})

	t.Run("reports no expiry for an opaque token", func(t *testing.T) {
		// Providers may issue opaque access tokens; those carry no readable exp
		// and must fall back to the configured interval alone.
		_, ok := tokenExpiry("opaque-reference-token")

		assert.False(t, ok)
	})

	t.Run("reports no expiry for a malformed payload segment", func(t *testing.T) {
		for _, token := range []string{
			"header.!!!not-base64!!!.signature",
			"header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".signature",
			"header." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":"soon"}`)) + ".signature",
		} {
			_, ok := tokenExpiry(token)
			assert.False(t, ok, token)
		}
	})

	t.Run("accepts a non-integer exp", func(t *testing.T) {
		// RFC 7519 NumericDate permits a fractional value.
		want := time.Now().Add(time.Hour).Truncate(time.Second)
		payload := fmt.Sprintf(`{"exp":%d.75}`, want.Unix())
		token := "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"

		got, ok := tokenExpiry(token)

		require.True(t, ok)
		assert.WithinDuration(t, want, got, time.Second)
	})

	t.Run("reports no expiry for a JWT without an exp claim", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "someone"})
		signed, err := token.SignedString([]byte("irrelevant"))
		require.NoError(t, err)

		_, ok := tokenExpiry(signed)

		assert.False(t, ok)
	})
}

func TestCacheTTL(t *testing.T) {
	t.Run("uses the configured interval when the token outlives it", func(t *testing.T) {
		token := signedTokenWithExp(t, time.Now().Add(time.Hour))

		assert.Equal(t, 5*time.Minute, cacheTTL(5*time.Minute, token))
	})

	t.Run("caps the interval at the token expiry", func(t *testing.T) {
		token := signedTokenWithExp(t, time.Now().Add(30*time.Second))

		ttl := cacheTTL(5*time.Minute, token)

		assert.Greater(t, ttl, 20*time.Second)
		assert.LessOrEqual(t, ttl, 30*time.Second)
	})

	t.Run("returns zero for an already expired token", func(t *testing.T) {
		token := signedTokenWithExp(t, time.Now().Add(-time.Minute))

		assert.Zero(t, cacheTTL(5*time.Minute, token))
	})

	t.Run("falls back to the interval for an opaque token", func(t *testing.T) {
		assert.Equal(t, 5*time.Minute, cacheTTL(5*time.Minute, "opaque"))
	})

	t.Run("returns zero when caching is disabled", func(t *testing.T) {
		token := signedTokenWithExp(t, time.Now().Add(time.Hour))

		assert.Zero(t, cacheTTL(0, token))
		assert.Zero(t, cacheTTL(-time.Second, token))
	})
}

func TestValidationCache(t *testing.T) {
	info := &userInfoResponse{Username: "someone", Groups: []string{"group1"}}

	t.Run("returns a stored entry within its lifetime", func(t *testing.T) {
		cache := newValidationCache(time.Minute)
		cache.put("token", info, nil)

		entry, ok := cache.get("token")

		require.True(t, ok)
		assert.Equal(t, info, entry.info)
		assert.NoError(t, entry.err)
	})

	t.Run("misses for a token that was never stored", func(t *testing.T) {
		cache := newValidationCache(time.Minute)

		_, ok := cache.get("token")

		assert.False(t, ok)
	})

	t.Run("does not leak the raw token as a map key", func(t *testing.T) {
		cache := newValidationCache(time.Minute)
		cache.put("super-secret-token", info, nil)

		for key := range cache.entries {
			assert.NotContains(t, key, "super-secret-token")
		}
	})

	t.Run("expires an entry once its lifetime elapses", func(t *testing.T) {
		cache := newValidationCache(10 * time.Millisecond)
		cache.put("token", info, nil)

		time.Sleep(20 * time.Millisecond)

		_, ok := cache.get("token")
		assert.False(t, ok)
	})

	t.Run("a stored decision cannot outlive the token it describes", func(t *testing.T) {
		// The security property of the whole design, asserted through put rather than
		// through cacheTTL alone: storing the interval instead of the capped lifetime
		// would keep every cacheTTL test green while letting a revoked token retain
		// read access for the full interval. The horizon is whole seconds because a
		// JWT exp claim has second granularity.
		cache := newValidationCache(time.Minute)
		shortLived := signedTokenWithExp(t, time.Now().Add(2*time.Second))

		cache.put(shortLived, info, nil)
		cache.put("opaque-token", info, nil)

		capped, ok := cache.get(shortLived)
		require.True(t, ok)
		assert.LessOrEqual(t, time.Until(capped.expiresAt), 2*time.Second,
			"the decision must expire with the token, not with the interval")

		uncapped, ok := cache.get("opaque-token")
		require.True(t, ok)
		assert.Greater(t, time.Until(uncapped.expiresAt), 2*time.Second,
			"a token with no readable expiry must still get the full interval")
	})

	t.Run("stores nothing for an already expired token", func(t *testing.T) {
		cache := newValidationCache(time.Minute)
		expired := signedTokenWithExp(t, time.Now().Add(-time.Minute))

		cache.put(expired, info, nil)

		_, ok := cache.get(expired)
		assert.False(t, ok)
		assert.Empty(t, cache.entries)
	})

	t.Run("stores nothing when caching is disabled", func(t *testing.T) {
		cache := newValidationCache(0)
		cache.put("token", info, nil)

		_, ok := cache.get("token")
		assert.False(t, ok)
		assert.Empty(t, cache.entries)
	})

	t.Run("remembers a rejection so a bad token does not hammer the provider", func(t *testing.T) {
		cache := newValidationCache(time.Minute)
		rejection := errors.New("token validation failed with status: 401")

		cache.put("token", nil, rejection)

		entry, ok := cache.get("token")
		require.True(t, ok)
		assert.Equal(t, rejection, entry.err)
		assert.Nil(t, entry.info)
	})

	t.Run("keeps a rejection for no longer than the negative TTL", func(t *testing.T) {
		// A long configured interval must not pin a rejection for minutes: a user
		// whose group membership was just fixed should recover quickly.
		cache := newValidationCache(time.Hour)
		cache.put("token", nil, errors.New("rejected"))

		entry, ok := cache.get("token")
		require.True(t, ok)
		assert.LessOrEqual(t, time.Until(entry.expiresAt), negativeCacheTTL)
	})

	t.Run("remembers a rejection for an already expired token", func(t *testing.T) {
		// The commonest bad credential. Deriving its lifetime from the token's own
		// expiry would refuse to cache it, and a loop of expired tokens would then
		// reach the provider on every single request.
		cache := newValidationCache(time.Minute)
		expired := signedTokenWithExp(t, time.Now().Add(-time.Hour))

		cache.put(expired, nil, errors.New("token validation failed with status: 401"))

		entry, ok := cache.get(expired)
		require.True(t, ok)
		assert.Error(t, entry.err)
	})

	t.Run("keeps a rejection no longer than the configured interval", func(t *testing.T) {
		cache := newValidationCache(time.Second)
		cache.put("token", nil, errors.New("rejected"))

		entry, ok := cache.get("token")
		require.True(t, ok)
		assert.LessOrEqual(t, time.Until(entry.expiresAt), time.Second)
	})

	t.Run("stops growing once the entry ceiling is reached", func(t *testing.T) {
		// Without a ceiling, a flood of distinct bogus tokens would grow the map
		// without bound — an unauthenticated memory-exhaustion primitive.
		cache := newValidationCache(time.Minute)
		for i := 0; i < maxCacheEntries+100; i++ {
			cache.put(fmt.Sprintf("token-%d", i), info, nil)
		}

		assert.LessOrEqual(t, len(cache.entries), maxCacheEntries)
	})

	t.Run("evicts to admit a new entry when full of unexpired ones", func(t *testing.T) {
		// A flood of unexpired junk must not lock real sessions out of the cache:
		// refusing to store would send a provider round trip on every UI request.
		cache := newValidationCache(time.Hour)
		for i := 0; i < maxCacheEntries; i++ {
			cache.put(fmt.Sprintf("flood-%d", i), nil, errors.New("rejected"))
		}

		cache.put("real-session-token", info, nil)

		entry, ok := cache.get("real-session-token")
		require.True(t, ok)
		assert.Equal(t, info, entry.info)
		assert.LessOrEqual(t, len(cache.entries), maxCacheEntries)
	})

	t.Run("reclaims expired entries to make room", func(t *testing.T) {
		cache := newValidationCache(10 * time.Millisecond)
		for i := 0; i < maxCacheEntries; i++ {
			cache.put(string(rune(i))+"-token", info, nil)
		}
		time.Sleep(20 * time.Millisecond)

		cache.put("fresh-token", info, nil)

		_, ok := cache.get("fresh-token")
		assert.True(t, ok, "an expired cache must not stay full forever")
	})
}
