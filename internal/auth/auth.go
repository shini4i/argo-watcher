package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/config"
)

// AuthStrategy defines the behaviour required for a token validation strategy.
type AuthStrategy interface {
	Validate(token string) (bool, error)
}

// Authenticator coordinates multiple AuthStrategy implementations against an HTTP request.
type Authenticator struct {
	strategies map[string]AuthStrategy
}

// NewAuthenticator builds an Authenticator instance using the provided strategies map.
func NewAuthenticator(strategies map[string]AuthStrategy) *Authenticator {
	normalized := make(map[string]AuthStrategy, len(strategies))
	for header, strategy := range strategies {
		if strategy == nil {
			continue
		}
		normalized[header] = strategy
	}

	return &Authenticator{
		strategies: normalized,
	}
}

// parseAuthToken extracts and normalizes a token from the given request header.
// It strips the "Bearer " prefix if present and returns an empty string for missing or empty tokens.
func parseAuthToken(request *http.Request, header string) string {
	token := request.Header.Get(header)
	if token == "" {
		return ""
	}

	if after, found := strings.CutPrefix(token, "Bearer "); found {
		token = after
	}

	return token
}

// Validate walks through all registered strategies and validates any matching
// token on the request.
//
// Three return states callers must distinguish:
//   - (true, nil)  — a valid token was found.
//   - (false, nil) — no auth header was sent (or all matching headers were
//     empty); no strategy was actually invoked. Callers should treat this
//     as "authentication not provided", not "wrong token".
//   - (false, err) — at least one strategy was invoked and rejected the
//     token; err is the last strategy's reason and should be surfaced to
//     the client so it can show something actionable.
func (a *Authenticator) Validate(request *http.Request) (bool, error) {
	return a.walk(request, func(strategy AuthStrategy, token string) (bool, error) {
		return strategy.Validate(token)
	})
}

// AuthenticateRequest reports whether the request carries a credential proving
// the caller is authenticated, without requiring any privilege.
//
// It follows the same three return states as Validate, so callers can tell "no
// credential sent" from "credential rejected".
func (a *Authenticator) AuthenticateRequest(request *http.Request) (bool, error) {
	return a.walk(request, func(strategy AuthStrategy, token string) (bool, error) {
		// A strategy that separates authentication from authorization exposes
		// Authenticate for the weaker check — currently OIDC, whose Validate
		// additionally demands privileged-group membership. A strategy with no group
		// concept (the deploy token, a CI JWT) does not, because for it a valid token
		// answers both questions, so Validate is the authentication check too.
		authenticator, ok := strategy.(interface{ Authenticate(token string) error })
		if !ok {
			return strategy.Validate(token)
		}

		if err := authenticator.Authenticate(token); err != nil {
			return false, err
		}
		return true, nil
	})
}

// walk applies check to every strategy whose header carries a token, returning on
// the first acceptance. When no strategy accepts, it returns a nil error if no
// strategy was invoked at all — the "authentication not provided" state described
// on Validate — and otherwise the failure of the invoked strategies.
//
// A request may carry several credentials at once — the Web UI sends its OIDC token in
// both Authorization and Oidc-Authorization, and the JWT strategy rejects that copy —
// so when outcomes disagree ErrProviderUnavailable wins: "rejected" is only truthful
// if every credential was evaluated, and callers turn a rejection into a 401. Map
// iteration is randomized, so without this precedence the status flips between runs.
func (a *Authenticator) walk(request *http.Request, check func(AuthStrategy, string) (bool, error)) (bool, error) {
	if a == nil || request == nil {
		return false, nil
	}

	var rejectedErr, unavailableErr error

	for header, strategy := range a.strategies {
		token := parseAuthToken(request, header)
		if token == "" {
			continue
		}

		valid, err := check(strategy, token)
		if valid {
			return true, nil
		}
		if errors.Is(err, ErrProviderUnavailable) {
			unavailableErr = err
			continue
		}
		if err != nil {
			rejectedErr = err
		}
	}

	if unavailableErr != nil {
		return false, unavailableErr
	}

	return false, rejectedErr
}

// ValidateStrategy restricts validation to a single allowed strategy header.
// Only the strategy registered under allowedHeader is considered; all other headers are skipped.
func (a *Authenticator) ValidateStrategy(request *http.Request, allowedHeader string) (bool, error) {
	if a == nil || request == nil {
		return false, nil
	}

	strategy, ok := a.strategies[allowedHeader]
	if !ok {
		return false, nil
	}

	token := parseAuthToken(request, allowedHeader)
	if token == "" {
		return false, nil
	}

	return strategy.Validate(token)
}

// Strategy returns a specific AuthStrategy by header key if it exists.
func (a *Authenticator) Strategy(header string) (AuthStrategy, bool) {
	if a == nil {
		return nil, false
	}

	strategy, ok := a.strategies[header]
	return strategy, ok
}

// NewOIDCAuthService initializes a new OIDC authentication service using the given server config.
// It validates the issuer URL and returns an error if the config is nil or the URL is malformed.
func NewOIDCAuthService(config *config.ServerConfig) (*OIDCAuthService, error) {
	if config == nil {
		return nil, fmt.Errorf("server config must not be nil")
	}

	oidcAuthService := &OIDCAuthService{}
	if err := oidcAuthService.Init(
		config.OIDC.IssuerURL,
		config.OIDC.ClientId,
		config.OIDC.PrivilegedGroups,
		time.Duration(config.OIDC.TokenValidationInterval)*time.Millisecond,
	); err != nil {
		return nil, err
	}
	return oidcAuthService, nil
}

// NewDeployTokenAuthService initializes a new deploy token authentication service.
// Accepts a token string and returns a pointer to a DeployTokenAuthService.
func NewDeployTokenAuthService(token string) *DeployTokenAuthService {
	return &DeployTokenAuthService{
		token: token,
	}
}

// NewJWTAuthService initializes a new JWT authentication service.
// It takes a secret key string and returns a pointer to a JWTAuthService.
func NewJWTAuthService(secret string) *JWTAuthService {
	return &JWTAuthService{
		secretKey: []byte(secret),
	}
}

var (
	_ AuthStrategy = (*OIDCAuthService)(nil)
	_ AuthStrategy = (*DeployTokenAuthService)(nil)
	_ AuthStrategy = (*JWTAuthService)(nil)
)
