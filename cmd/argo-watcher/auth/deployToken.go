package auth

import "fmt"

type DeployTokenAuthService struct {
	Token string
}

func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	tokenIsValid := s.Token == token
	if !tokenIsValid {
		return false, fmt.Errorf("deploy token is either missing or invalid")
	}
	return tokenIsValid, nil
}
