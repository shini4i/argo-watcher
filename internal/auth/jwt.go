package auth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuthService validates the HMAC-signed tokens a CI pipeline presents. Beyond the
// signature it enforces the claim policy assembled in NewJWTAuthService.
type JWTAuthService struct {
	secretKey []byte
	options   []jwt.ParserOption
}

// Validate verifies a JSON Web Token, checking signature and claims.
func (j *JWTAuthService) Validate(tokenStr string) (bool, error) {
	if tokenStr == "" {
		return false, fmt.Errorf("empty token")
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	}, j.options...)

	if err != nil {
		return false, err
	}

	if !token.Valid {
		return false, errors.New("invalid token")
	}

	return true, nil
}
