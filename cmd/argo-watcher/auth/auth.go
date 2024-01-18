package auth

type ExternalAuthService interface {
	Init(url, realm, clientId string)
	Validate(token string) (bool, error)
}

func NewExternalAuthService() ExternalAuthService {
	return &KeycloakAuthService{}
}
