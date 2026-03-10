package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
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

// Validate walks through all registered strategies and validates any matching token on the request.
func (a *Authenticator) Validate(request *http.Request) (bool, error) {
	if a == nil || request == nil {
		return false, nil
	}

	var lastErr error

	for header, strategy := range a.strategies {
		token := parseAuthToken(request, header)
		if token == "" {
			continue
		}

		valid, err := strategy.Validate(token)
		if valid {
			return true, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	return false, lastErr
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

// NewKeycloakAuthService initializes a new Keycloak authentication service using the given server config.
// It validates the Keycloak URL and returns an error if the config is nil or the URL is malformed.
func NewKeycloakAuthService(config *config.ServerConfig) (*KeycloakAuthService, error) {
	if config == nil {
		return nil, fmt.Errorf("server config must not be nil")
	}

	keycloakAuthService := &KeycloakAuthService{}
	if err := keycloakAuthService.Init(
		config.Keycloak.Url,
		config.Keycloak.Realm,
		config.Keycloak.ClientId,
		config.Keycloak.PrivilegedGroups,
	); err != nil {
		return nil, err
	}
	return keycloakAuthService, nil
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
	_ AuthStrategy = (*KeycloakAuthService)(nil)
	_ AuthStrategy = (*DeployTokenAuthService)(nil)
	_ AuthStrategy = (*JWTAuthService)(nil)
)
