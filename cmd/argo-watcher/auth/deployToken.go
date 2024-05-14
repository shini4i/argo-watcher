package auth

import "fmt"

// DeployTokenAuthService is a struct that maintains and validates deploy tokens.
type DeployTokenAuthService struct {
	// token is the deploy token string used for validation.
	token string
}

// Validate checks if the provided token matches the stored deploy token
// Returns a boolean indicating whether the token is valid, and any errors encountered.
func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	tokenIsValid := s.token == token
	if !tokenIsValid {
		return false, fmt.Errorf("deploy token is either missing or invalid")
	}
	return tokenIsValid, nil
}
