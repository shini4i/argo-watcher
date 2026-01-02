package auth

import (
	"errors"
	"fmt"
	"time"

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

	// Explicitly verify token validity after parsing
	if !token.Valid {
		return false, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false, errors.New("invalid claims type")
	}

	if _, exists := claims["exp"]; !exists {
		return false, errors.New("missing exp claim")
	}

	// Validate "iat" (issued at) claim
	if iatVal, ok := claims["iat"].(float64); ok {
		if time.Now().Unix() < int64(iatVal) {
			return false, errors.New("token used before issued")
		}
	}

	return true, nil
}
