package auth

import (
	"crypto/subtle"
	"fmt"
)

// DeployTokenAuthService validates deploy tokens.
type DeployTokenAuthService struct {
	token string
}

// Validate checks the provided token against the stored deploy token. The
// authenticator only invokes it with a non-empty token, so the failure mode is
// always "wrong value", never "missing" — the error wording reflects that.
//
// The comparison is constant-time so the rejection tells an attacker nothing about
// how much of the token they guessed right.
func (s *DeployTokenAuthService) Validate(token string) (bool, error) {
	if subtle.ConstantTimeCompare([]byte(s.token), []byte(token)) != 1 {
		return false, fmt.Errorf("deploy token is invalid")
	}
	return true, nil
}
