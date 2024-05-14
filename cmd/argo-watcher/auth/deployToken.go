package auth

import "fmt"

type DeployTokenAuthService struct {
	token string
}

func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	tokenIsValid := s.token == token
	if !tokenIsValid {
		return false, fmt.Errorf("deploy token is either missing or invalid")
	}
	return tokenIsValid, nil
}
