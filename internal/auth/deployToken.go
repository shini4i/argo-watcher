package auth

import "fmt"

// DeployTokenAuthService is a struct that maintains and validates deploy tokens.
type DeployTokenAuthService struct {
	// token is the deploy token string used for validation.
	token string
}

// Validate checks if the provided token matches the stored deploy token.
// Returns a boolean indicating whether the token is valid, and an error
// describing why when it is not. Note: this method is only invoked by the
// authenticator with a non-empty token, so the failure mode is always
// "wrong value", never "missing" — the wording reflects that.
func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	if s.token != token {
		return false, fmt.Errorf("deploy token is invalid")
	}
	return true, nil
}
