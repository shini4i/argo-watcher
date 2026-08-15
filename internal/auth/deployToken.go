package auth

import "fmt"

// DeployTokenAuthService validates deploy tokens.
type DeployTokenAuthService struct {
	token string
}

// Validate checks the provided token against the stored deploy token. The
// authenticator only invokes it with a non-empty token, so the failure mode is
// always "wrong value", never "missing" — the error wording reflects that.
func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	if s.token != token {
		return false, fmt.Errorf("deploy token is invalid")
	}
	return true, nil
}
