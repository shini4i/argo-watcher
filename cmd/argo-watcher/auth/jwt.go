package auth

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
)

// JWTAuthService manages JSON Web Token authentication.
type JWTAuthService struct {
	secretKey []byte
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
	})

	if err != nil {
		return false, err
	}

	if !token.Valid {
		return false, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false, fmt.Errorf("invalid token claims")
	}

	if _, exists := claims["exp"]; !exists {
		return false, fmt.Errorf("missing exp claim")
	}

	return true, nil
}
