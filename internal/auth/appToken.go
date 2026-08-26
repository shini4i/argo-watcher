package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shini4i/argo-watcher/internal/apptoken"
)

// AppTokenAuthService validates application deploy tokens against the store that
// issued them. It alone answers a question about a *named application*: the scope
// is set server-side at issue time and the holder cannot assert anything about it,
// so the unscoped Validate refuses rather than treating the token as global.
type AppTokenAuthService struct {
	store apptoken.Store
}

// NewAppTokenAuthService initializes an application deploy token strategy over the
// given store.
func NewAppTokenAuthService(store apptoken.Store) *AppTokenAuthService {
	return &AppTokenAuthService{store: store}
}

// Validate refuses: an application deploy token authorizes named applications, and
// there is nothing here to weigh its scope against. Privileged endpoints are
// restricted to the OIDC strategy anyway, so this path only guards against a
// future caller reaching for the wrong method.
func (s *AppTokenAuthService) Validate(_ string) (bool, error) {
	return false, errors.New("an application deploy token cannot authorize a request with no application context")
}

// Authenticate reports whether the token identifies a live credential, without
// asking which applications it covers, because a read names no application to judge
// a scope against. Note this grants the whole task list, not only the token's own
// applications — the same breadth a CI JWT already has.
func (s *AppTokenAuthService) Authenticate(token string) error {
	_, err := s.resolve(token)
	return err
}

// ValidateForApp reports whether the token authorizes a deployment of the named
// application, recording the use when it does.
func (s *AppTokenAuthService) ValidateForApp(token, app string) (bool, error) {
	if app == "" {
		return false, errors.New("the request names no application, so an application deploy token cannot authorize it")
	}

	stored, err := s.resolve(token)
	if err != nil {
		return false, err
	}

	if !stored.Scope.Allows(app) {
		return false, fmt.Errorf("this application deploy token does not authorize application %s (its scope is: %s)", app, stored.Scope)
	}

	// Bookkeeping for finding tokens nothing uses. It must never fail a deployment,
	// so a write failure is logged and the authorization stands.
	if err := s.store.MarkUsed(stored.Id); err != nil {
		slog.Warn("failed to record use of an application deploy token", "token_id", stored.Id, "error", err)
	}

	return true, nil
}

// resolve finds the token behind a secret and confirms it may still be used. A
// store failure becomes ErrProviderUnavailable rather than a rejection, so a
// database blip answers 503 instead of telling a pipeline its credential is bad.
// Revocation and expiry are reported apart: the holder can act on the difference.
func (s *AppTokenAuthService) resolve(token string) (*apptoken.Token, error) {
	stored, err := s.store.Lookup(token)
	if errors.Is(err, apptoken.ErrNotFound) {
		return nil, errors.New("the presented credential is not a known application deploy token")
	}
	if err != nil {
		slog.Error("app token: store lookup failed", "error", err)
		return nil, ErrProviderUnavailable
	}

	if !stored.RevokedAt.IsZero() {
		return nil, errors.New("this application deploy token has been revoked")
	}
	if !stored.Usable(time.Now()) {
		return nil, errors.New("this application deploy token has expired")
	}

	return stored, nil
}

// disabledAppTokens rejects application deploy tokens with the configuration they
// need. Without it the token would be an unreadable header: the task would be
// accepted as uncredentialed, and only the skipped write-back would hint at why
// the rollout then failed.
type disabledAppTokens struct{}

// DisabledAppTokenStrategy returns the stand-in used while application deploy
// tokens are unavailable.
func DisabledAppTokenStrategy() AuthStrategy {
	return disabledAppTokens{}
}

func (disabledAppTokens) Validate(_ string) (bool, error) {
	return false, errors.New("application deploy tokens require OIDC_ENABLED and STATE_TYPE=postgres")
}

func (d disabledAppTokens) Authenticate(token string) error {
	_, err := d.Validate(token)
	return err
}

func (d disabledAppTokens) ValidateForApp(token, _ string) (bool, error) {
	return d.Validate(token)
}

// PrefixRouter puts two credential types on one header. Application deploy tokens
// and CI JWTs both arrive in Authorization, and the strategy map is keyed by
// header, so it cannot hold one entry for each. This routes on the token's shape:
// apptoken.Prefix goes to the prefixed strategy, anything else to the fallback.
type PrefixRouter struct {
	prefixed AuthStrategy
	fallback AuthStrategy
}

// NewPrefixRouter builds a router over the two strategies. A nil fallback means
// nothing evaluates a non-prefixed token, which is reported as ErrNoCredential —
// the header is then ignored, exactly as it was before any strategy read it.
func NewPrefixRouter(prefixed, fallback AuthStrategy) *PrefixRouter {
	return &PrefixRouter{prefixed: prefixed, fallback: fallback}
}

// strategyFor picks the strategy a token's shape names.
func (r *PrefixRouter) strategyFor(token string) AuthStrategy {
	if apptoken.HasPrefix(token) {
		return r.prefixed
	}

	return r.fallback
}

// Handles reports whether any strategy would evaluate a token of this shape, which
// a bare header-presence check cannot tell apart from a header nobody reads.
func (r *PrefixRouter) Handles(token string) bool {
	return r.strategyFor(token) != nil
}

func (r *PrefixRouter) Validate(token string) (bool, error) {
	strategy := r.strategyFor(token)
	if strategy == nil {
		return false, ErrNoCredential
	}

	return strategy.Validate(token)
}

func (r *PrefixRouter) Authenticate(token string) error {
	strategy := r.strategyFor(token)
	if strategy == nil {
		return ErrNoCredential
	}

	if _, err := authenticate(strategy, token); err != nil {
		return err
	}

	return nil
}

func (r *PrefixRouter) ValidateForApp(token, app string) (bool, error) {
	strategy := r.strategyFor(token)
	if strategy == nil {
		return false, ErrNoCredential
	}

	return validateForApp(strategy, token, app)
}

var (
	_ AuthStrategy = (*AppTokenAuthService)(nil)
	_ AuthStrategy = (*PrefixRouter)(nil)
	_ AuthStrategy = disabledAppTokens{}
)
