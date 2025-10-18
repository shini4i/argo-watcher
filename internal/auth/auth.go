package auth

import (
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

// Validate walks through all registered strategies and validates any matching token on the request.
func (a *Authenticator) Validate(request *http.Request) (bool, error) {
	if a == nil || request == nil {
		return false, nil
	}

	var lastErr error

	for header, strategy := range a.strategies {
		token := request.Header.Get(header)
		if token == "" {
			continue
		}

		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
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

// Strategy returns a specific AuthStrategy by header key if it exists.
func (a *Authenticator) Strategy(header string) (AuthStrategy, bool) {
	if a == nil {
		return nil, false
	}

	strategy, ok := a.strategies[header]
	return strategy, ok
}

// NewKeycloakAuthService initializes a new Keycloak authentication service using the given server config.
// It takes a ServerConfig pointer as input and returns a pointer to a KeycloakAuthService.
func NewKeycloakAuthService(config *config.ServerConfig) *KeycloakAuthService {
	keycloakAuthService := &KeycloakAuthService{}
	keycloakAuthService.Init(
		config.Keycloak.Url,
		config.Keycloak.Realm,
		config.Keycloak.ClientId,
		config.Keycloak.PrivilegedGroups,
	)
	return keycloakAuthService
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
