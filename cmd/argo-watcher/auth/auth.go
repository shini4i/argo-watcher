package auth

import "github.com/shini4i/argo-watcher/cmd/argo-watcher/config"

type AuthService interface {
	Validate(token string) (bool, error)
}

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

func NewDeployTokenAuthService(token string) *DeployTokenAuthService {
	return &DeployTokenAuthService{
		token: token,
	}
}

func NewJWTAuthService(secret string) *JWTAuthService {
	return &JWTAuthService{
		secretKey: secret,
	}
}
