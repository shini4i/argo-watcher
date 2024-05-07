package auth

type AuthService interface {
	Validate(token string) (bool, error)
}

func NewKeycloakAuthService() *KeycloakAuthService {
	return &KeycloakAuthService{}
}

func NewDeployTokenAuthService(token string) *DeployTokenAuthService {
	return &DeployTokenAuthService{
		Token: token,
	}
}
