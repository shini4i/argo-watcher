package auth

import (
	"crypto/subtle"
	"fmt"
)

// DeployTokenAuthService validates deploy tokens.
type DeployTokenAuthService struct {
	token string
}

// Validate checks the provided token against the stored deploy token, constant-time so
// the rejection tells an attacker nothing about how much they guessed right. The
// authenticator only invokes it with a non-empty token, so the failure mode is always
// "wrong value", never "missing" — the error wording reflects that.
func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	// Two empty strings compare equal, so a service built without a deploy token would
	// authorize a request that presents none.
	if s.token == "" || token == "" {
		return false, fmt.Errorf("deploy token is invalid")
	}

	if subtle.ConstantTimeCompare([]byte(s.token), []byte(token)) != 1 {
		return false, fmt.Errorf("deploy token is invalid")
	}
	return true, nil
}
