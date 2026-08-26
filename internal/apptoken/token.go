// Package apptoken issues and validates application deploy tokens: opaque,
// revocable credentials authorizing a git write-back for a named set of
// applications. A token carries no claims — scope, expiry and revocation live in
// the row keyed by its hash, so revoking one takes effect on the next request.
package apptoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Prefix marks a string as an application deploy token. It is what lets a single
// Authorization header carry either credential: the server routes on this prefix
// and hands anything else to the JWT parser. It is also the pattern a secret
// scanner registers, so a token pasted into a repository can be detected.
const Prefix = "awt_"

// secretBytes is the entropy behind a token. 256 bits leaves nothing to
// brute-force, which is why the stored hash is a plain SHA-256 rather than a
// password KDF: there is no low-entropy secret for a KDF to protect.
const secretBytes = 32

// hintLength is how many trailing characters of a token are stored in clear, so
// the UI can identify a token it can never show again.
const hintLength = 4

// MaxApps caps an explicit application list. A token meant for more applications
// than this is really an all-apps token, and saying so is more honest than a list
// nobody reads. It mirrors models.MaxTaskImages in intent: a sanity bound on a
// field that is read back on every request.
const MaxApps = 200

// MaxAppNameLength bounds a single entry of the application list. It matches
// models.MaxTaskFieldLength, since the entries are compared against a task's
// application name.
const MaxAppNameLength = 255

// ErrNotFound reports that no token matches the presented secret. Callers must
// treat it as a rejection, never as a backend failure.
var ErrNotFound = errors.New("application deploy token not found")

// Scope names the applications a token authorizes. AllApps and a non-empty Apps
// are mutually exclusive — the database rejects the combination — because a token
// that is both bounded and unbounded has no meaning.
type Scope struct {
	// Apps is the explicit list of application names, empty when AllApps is set.
	Apps []string
	// AllApps authorizes every application. It is a distinct field rather than a
	// wildcard entry in Apps so that granting the whole estate cannot be the
	// accidental result of building the list wrongly.
	AllApps bool
}

// Allows reports whether the scope covers the named application. Comparison is
// exact: application names are Kubernetes object names, which are already
// lowercase, so case folding would only widen the scope beyond what was granted.
func (s Scope) Allows(app string) bool {
	if s.AllApps {
		return true
	}

	return slices.Contains(s.Apps, app)
}

// String renders the scope for logs and error messages.
func (s Scope) String() string {
	if s.AllApps {
		return "all applications"
	}

	return strings.Join(s.Apps, ", ")
}

// Validate normalizes the scope and reports why it cannot be stored. It trims and
// deduplicates the application list, so the same scope always round-trips to the
// same row and Allows cannot be defeated by stray whitespace.
func (s *Scope) Validate() error {
	if s.AllApps {
		if len(s.Apps) > 0 {
			return errors.New("a token scoped to all applications must not also list applications")
		}
		s.Apps = nil
		return nil
	}

	seen := make(map[string]struct{}, len(s.Apps))
	normalized := make([]string, 0, len(s.Apps))

	for _, app := range s.Apps {
		app = strings.TrimSpace(app)
		if app == "" {
			return errors.New("an application name must not be empty")
		}
		if len(app) > MaxAppNameLength {
			return fmt.Errorf("an application name must be at most %d characters", MaxAppNameLength)
		}
		if _, duplicate := seen[app]; duplicate {
			continue
		}
		seen[app] = struct{}{}
		normalized = append(normalized, app)
	}

	if len(normalized) == 0 {
		return errors.New("a token must be scoped either to at least one application or to all of them")
	}
	if len(normalized) > MaxApps {
		return fmt.Errorf("a token must not list more than %d applications; scope it to all applications instead", MaxApps)
	}

	s.Apps = normalized
	return nil
}

// Token is a stored token's metadata. The secret itself is never part of it: it
// exists in clear exactly once, in the response to the request that created it.
type Token struct {
	Id          uuid.UUID
	Scope       Scope
	Hint        string
	Description string
	CreatedBy   string
	CreatedAt   time.Time
	// ExpiresAt is zero when the token never expires.
	ExpiresAt time.Time
	// RevokedAt is zero while the token is live. A revoked token keeps its row so
	// the audit trail of who issued it, and when it was withdrawn, survives.
	RevokedAt time.Time
	// LastUsedAt is zero until the token authorizes a deployment. It is recorded
	// only on that path, not on reads, so a polled status endpoint does not turn
	// every request into a write.
	LastUsedAt time.Time
}

// Usable reports whether the token may still authorize anything at the given
// instant. now is a parameter so the caller decides the clock, which keeps the
// expiry boundary testable.
func (t *Token) Usable(now time.Time) bool {
	if t == nil {
		return false
	}
	if !t.RevokedAt.IsZero() {
		return false
	}

	return t.ExpiresAt.IsZero() || now.Before(t.ExpiresAt)
}

// Credential is a freshly minted token: the Secret to hand to the operator once,
// and the values stored in its place.
type Credential struct {
	// Secret is the only copy of the token. Nothing persists it.
	Secret string
	// Hash is the SHA-256 of Secret, the value the token is looked up by.
	Hash []byte
	// Hint is the last few characters of Secret, shown by the UI to identify a
	// token whose secret is gone.
	Hint string
}

// NewCredential mints a token from the system CSPRNG.
func NewCredential() (Credential, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return Credential{}, fmt.Errorf("failed to generate an application deploy token: %w", err)
	}

	secret := Prefix + base64.RawURLEncoding.EncodeToString(buf)

	return Credential{
		Secret: secret,
		Hash:   Hash(secret),
		Hint:   secret[len(secret)-hintLength:],
	}, nil
}

// HasPrefix reports whether the string is shaped like an application deploy
// token. It is the routing test, not a validity check: a prefixed string still
// has to match a live row.
func HasPrefix(token string) bool {
	return strings.HasPrefix(token, Prefix)
}

// Hash derives the value a token is stored and looked up by. A plain SHA-256 is
// the right primitive here — see secretBytes.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
