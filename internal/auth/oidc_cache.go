package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// maxCacheEntries bounds the validation cache. Without a ceiling, a flood of
	// distinct bogus tokens would grow the map without bound, turning an
	// unauthenticated endpoint into a memory-exhaustion primitive. At the ceiling,
	// expired entries are reclaimed and — failing that — an arbitrary entry is
	// evicted (see evictOneLocked), so the map never grows past it.
	maxCacheEntries = 10000

	// negativeCacheTTL caps how long a rejection is remembered, independently of
	// the configured interval. A long interval must not pin a rejection for
	// minutes: a token the provider refused because of clock skew, or a session an
	// administrator has just reinstated, needs to recover quickly. Its only job is
	// to stop a hot loop of bad requests from hammering the provider.
	negativeCacheTTL = 30 * time.Second
)

// cachedValidation is one remembered provider decision. Exactly one of info and
// err is set: info for an accepted token, err for a rejected one.
type cachedValidation struct {
	info      *userInfoResponse
	err       error
	expiresAt time.Time
}

// validationCache memoizes provider decisions per access token so that gating
// every request on OIDC costs one userinfo round trip per token per interval
// rather than one per request.
//
// Entries are keyed by the SHA-256 of the token, never the token itself, so raw
// credentials do not sit in a map key where a heap dump or a debug print would
// expose them. A zero or negative TTL disables caching entirely.
type validationCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cachedValidation
}

// newValidationCache builds a cache whose entries live for at most ttl. A
// non-positive ttl yields a cache that stores nothing.
func newValidationCache(ttl time.Duration) *validationCache {
	return &validationCache{
		ttl:     ttl,
		entries: make(map[string]cachedValidation),
	}
}

// cacheKey derives the storage key for a token.
func cacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// get returns the remembered decision for a token, or ok=false when there is
// none or it has expired.
func (c *validationCache) get(token string) (cachedValidation, bool) {
	if c == nil || c.ttl <= 0 {
		return cachedValidation{}, false
	}

	key := cacheKey(token)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return cachedValidation{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return cachedValidation{}, false
	}

	return entry, true
}

// put remembers a provider decision for a token. Accepted tokens live for the
// configured interval capped by the token's own expiry; rejections live for
// negativeCacheTTL, or the configured interval when that is shorter. Nothing is
// stored when caching is disabled or the computed lifetime is zero.
func (c *validationCache) put(token string, info *userInfoResponse, err error) {
	if c == nil || c.ttl <= 0 {
		return
	}

	ttl := c.entryTTL(token, err)
	if ttl <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= maxCacheEntries {
		c.reclaimExpiredLocked()
		c.evictOneLocked()
	}

	c.entries[cacheKey(token)] = cachedValidation{
		info:      info,
		err:       err,
		expiresAt: time.Now().Add(ttl),
	}
}

// entryTTL returns how long this decision may be remembered.
//
// A rejection's lifetime is deliberately NOT derived from the token's expiry. The
// most common bad credential is an expired token, whose remaining lifetime is zero
// or negative — deriving from it would refuse to cache exactly the rejection worth
// remembering, and a loop of expired tokens would reach the provider on every
// request. A rejected token stays rejected, so a short fixed lifetime is both safe
// and sufficient.
func (c *validationCache) entryTTL(token string, err error) time.Duration {
	if err == nil {
		return cacheTTL(c.ttl, token)
	}

	if c.ttl < negativeCacheTTL {
		return c.ttl
	}
	return negativeCacheTTL
}

// reclaimExpiredLocked drops every expired entry. The caller must hold the mutex.
func (c *validationCache) reclaimExpiredLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

// evictOneLocked frees a slot by dropping an arbitrary entry when the cache is
// still full after reclaiming expired ones. The caller must hold the mutex.
//
// Evicting beats refusing to store: a flood of distinct unusable tokens would
// otherwise hold every slot until it expired, leaving real sessions uncached and
// sending a provider round trip on every UI request. Arbitrary choice (Go map
// order) rather than least-recently-used — the bookkeeping LRU needs buys nothing
// here, since a wrongly evicted entry costs one round trip to rebuild. Note this
// bounds memory only: nothing here rate-limits how much provider traffic an
// unauthenticated caller can induce.
func (c *validationCache) evictOneLocked() {
	if len(c.entries) < maxCacheEntries {
		return
	}

	for key := range c.entries {
		delete(c.entries, key)
		return
	}
}

// cacheTTL returns how long a decision about the given token may be reused: the
// configured interval, shortened so a cached decision can never outlive the token
// it describes.
func cacheTTL(configured time.Duration, token string) time.Duration {
	if configured <= 0 {
		return 0
	}

	expiry, ok := tokenExpiry(token)
	if !ok {
		return configured
	}

	remaining := time.Until(expiry)
	if remaining <= 0 {
		return 0
	}
	if remaining < configured {
		return remaining
	}

	return configured
}

// tokenExpiry reads the "exp" claim of a JWT access token without verifying its
// signature, reporting ok=false for opaque tokens and for JWTs carrying no exp.
//
// The claim is used for exactly one purpose: shortening a cache entry's lifetime.
// It is never a substitute for validation — the provider remains the only
// authority on whether a token is good, and a decision only enters the cache
// after the provider has spoken. A token claiming a distant expiry therefore buys
// nothing beyond the configured interval.
func tokenExpiry(token string) (time.Time, bool) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
		return time.Time{}, false
	}

	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil {
		return time.Time{}, false
	}

	return expiry.Time, true
}
