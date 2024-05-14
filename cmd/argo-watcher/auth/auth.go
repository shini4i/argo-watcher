package auth

import "github.com/shini4i/argo-watcher/cmd/argo-watcher/config"

// AuthService is an interface for services that verify and validate tokens.
type AuthService interface {
	Validate(token string) (bool, error)
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
		secretKey: secret,
	}
}
