package auth

type DeployTokenAuthService struct {
	Token string
}

func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	return s.Token == token, nil
}
