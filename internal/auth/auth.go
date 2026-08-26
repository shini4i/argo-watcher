package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shini4i/argo-watcher/internal/config"
)

// AuthStrategy defines the behaviour required for a token validation strategy.
type AuthStrategy interface {
	Validate(token string) (bool, error)
}

// ErrNoCredential reports that no configured strategy evaluates a token of the
// presented shape, so the request carries nothing to judge. walk skips it instead
// of recording a rejection, which keeps a header no strategy reads behaving as it
// always has: ignored, not refused.
var ErrNoCredential = errors.New("no configured strategy handles this credential")

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

// ParseAuthToken extracts and normalizes a token from the given request header.
// It strips the "Bearer " prefix if present and returns an empty string for missing or empty tokens.
func ParseAuthToken(request *http.Request, header string) string {
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
	return a.walk(request, authenticate)
}

// ValidateForApp reports whether the request carries a credential authorizing a
// deployment of the named application, with the same three return states as
// Validate. Only application deploy tokens and JWTs carrying allowed_apps narrow
// their answer by application; every other credential authorizes every one.
func (a *Authenticator) ValidateForApp(request *http.Request, app string) (bool, error) {
	return a.walk(request, func(strategy AuthStrategy, token string) (bool, error) {
		return validateForApp(strategy, token, app)
	})
}

// AuthenticateToken authenticates a bare token against the strategy registered under
// header, for a transport that cannot carry one: a browser cannot set headers on a
// WebSocket handshake. It returns (false, nil) when the token is empty or no such
// strategy is registered, matching AuthenticateRequest's "no credential" state.
func (a *Authenticator) AuthenticateToken(header, token string) (bool, error) {
	if a == nil || token == "" {
		return false, nil
	}

	strategy, ok := a.strategies[header]
	if !ok {
		return false, nil
	}

	if after, found := strings.CutPrefix(token, "Bearer "); found {
		token = after
	}

	return authenticate(strategy, token)
}

// authenticate asks a strategy the authentication-only question. A strategy that
// separates authentication from authorization exposes Authenticate — currently OIDC,
// whose Validate additionally demands privileged-group membership. One with no group
// concept (the deploy token, a CI JWT) does not, because for it a valid token answers
// both questions.
func authenticate(strategy AuthStrategy, token string) (bool, error) {
	authenticator, ok := strategy.(interface{ Authenticate(token string) error })
	if !ok {
		return strategy.Validate(token)
	}

	if err := authenticator.Authenticate(token); err != nil {
		return false, err
	}

	return true, nil
}

// validateForApp asks a strategy the application-scoped question. One whose tokens
// are confined to named applications exposes ValidateForApp; one whose tokens
// authorize the whole estate does not, and answers the unscoped question instead.
func validateForApp(strategy AuthStrategy, token, app string) (bool, error) {
	scoped, ok := strategy.(interface {
		ValidateForApp(token, app string) (bool, error)
	})
	if !ok {
		return strategy.Validate(token)
	}

	return scoped.ValidateForApp(token, app)
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
		token := ParseAuthToken(request, header)
		if token == "" {
			continue
		}

		valid, err := check(strategy, token)
		if valid {
			return true, nil
		}
		if errors.Is(err, ErrNoCredential) {
			continue
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

// IdentifyRequest names the user behind the credential in the given header, for
// attributing a privileged action. It returns an empty name when no strategy is
// registered there, the strategy cannot name a user, or no token was sent.
func (a *Authenticator) IdentifyRequest(request *http.Request, header string) (string, error) {
	if a == nil || request == nil {
		return "", nil
	}

	strategy, ok := a.strategies[header]
	if !ok {
		return "", nil
	}

	identifier, ok := strategy.(interface {
		Identify(token string) (string, error)
	})
	if !ok {
		return "", nil
	}

	token := ParseAuthToken(request, header)
	if token == "" {
		return "", nil
	}

	return identifier.Identify(token)
}

// ValidateStrategy restricts validation to the strategy registered under allowedHeader.
func (a *Authenticator) ValidateStrategy(request *http.Request, allowedHeader string) (bool, error) {
	if a == nil || request == nil {
		return false, nil
	}

	strategy, ok := a.strategies[allowedHeader]
	if !ok {
		return false, nil
	}

	token := ParseAuthToken(request, allowedHeader)
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

// NewOIDCAuthService initializes an OIDC authentication service from the given server config.
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

// NewDeployTokenAuthService initializes a deploy token authentication service.
func NewDeployTokenAuthService(token string) *DeployTokenAuthService {
	return &DeployTokenAuthService{
		token: token,
	}
}

// NewJWTAuthService initializes a JWT authentication service. An empty issuer or
// audience leaves that claim unchecked, so a fleet minting tokens without it keeps
// working; a configured value is enforced strictly — a token missing the claim is
// rejected, not accepted. Roll the claim out to every pipeline before configuring it.
func NewJWTAuthService(secret, issuer, audience string) *JWTAuthService {
	// exp is required and a future iat rejected whatever the configuration: both were
	// enforced before the claim binding existed.
	options := []jwt.ParserOption{jwt.WithExpirationRequired(), jwt.WithIssuedAt()}

	if issuer != "" {
		options = append(options, jwt.WithIssuer(issuer))
	}

	if audience != "" {
		options = append(options, jwt.WithAudience(audience))
	}

	return &JWTAuthService{
		secretKey: []byte(secret),
		options:   options,
	}
}

var (
	_ AuthStrategy = (*OIDCAuthService)(nil)
	_ AuthStrategy = (*DeployTokenAuthService)(nil)
	_ AuthStrategy = (*JWTAuthService)(nil)
)
