package auth

type ExternalAuthService interface {
	Init(url, realm, clientId string, privilegedGroups []string)
	Validate(token string) (bool, error)
	allowedToRollback(groups []string) bool
}

func NewExternalAuthService() ExternalAuthService {
	return &KeycloakAuthService{}
}
