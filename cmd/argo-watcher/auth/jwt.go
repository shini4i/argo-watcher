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

	// Extract and validate claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return false, fmt.Errorf("invalid token claims")
	}

	// Validate standard claims
	if expVal, ok := claims["exp"]; ok {
		if exp, valid := expVal.(float64); valid {
			if time.Now().Unix() > int64(exp) {
				return false, errors.New("token expired")
			}
		} else {
			return false, errors.New("invalid exp claim type")
		}
	} else {
		return false, errors.New("missing exp claim")
	}

	return true, nil
}
