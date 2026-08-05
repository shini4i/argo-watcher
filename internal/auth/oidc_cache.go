package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// maxCacheEntries bounds the map so a flood of distinct bogus tokens cannot
	// exhaust memory; at the ceiling entries are reclaimed or evicted.
	maxCacheEntries = 10000

	// negativeCacheTTL caps how long a rejection is remembered regardless of the
	// configured interval: long enough to blunt a hot loop of bad requests, short
	// enough that a reinstated session recovers quickly.
	negativeCacheTTL = 30 * time.Second
)

// cachedValidation is one remembered provider decision. Exactly one of info and
// err is set: info for an accepted token, err for a rejected one.
type cachedValidation struct {
	info      *userInfoResponse
	err       error
	expiresAt time.Time
}

// validationCache memoizes provider decisions per access token, so gating requests on
// OIDC costs one userinfo round trip per token per interval rather than one per
// request. Entries are keyed by the token's SHA-256 so raw credentials never sit in a
// map key. A non-positive TTL disables caching.
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
// A rejection's lifetime is NOT derived from the token's expiry: the commonest bad
// credential is an expired token, so deriving it would refuse to cache the very
// rejection worth remembering.
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

// evictOneLocked frees a slot by dropping an arbitrary entry when the cache is still
// full after reclaiming expired ones. The caller must hold the mutex.
//
// Evicting beats refusing to store, which would leave real sessions uncached behind a
// flood of junk tokens. The choice is arbitrary rather than LRU because a wrong
// eviction costs one round trip. This bounds memory only, not provider traffic.
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

// tokenExpiry reads the "exp" claim from a JWT access token's payload segment,
// reporting ok=false for opaque tokens and for JWTs carrying no exp.
//
// It decodes rather than parses on purpose: the value is a hint used only to shorten a
// cache entry's lifetime, never to decide anything. Going through the parser would
// imply this validates the token — it does not, and it must not, since the provider is
// the only authority on that and has already spoken by the time an entry is written.
func tokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}

	payload, err := jwt.NewParser().DecodeSegment(parts[1])
	if err != nil {
		return time.Time{}, false
	}

	// Seconds since the epoch, per RFC 7519 NumericDate; float because the spec allows
	// a non-integer value. Sub-second precision is irrelevant to a cache lifetime.
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}

	return time.Unix(int64(claims.Exp), 0), true
}
