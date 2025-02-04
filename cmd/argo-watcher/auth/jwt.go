package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuthService manages JWT authentication.
type JWTAuthService struct {
	secretKey []byte
}

// Init initializes the JWTAuthService with a provided secret key.
func (j *JWTAuthService) Init(secretKey string) {
	j.secretKey = []byte(secretKey) // Convert to byte slice for security
}

// Validate verifies a JWT token, checking signature and claims.
func (j *JWTAuthService) Validate(tokenStr string) (bool, error) {
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
		return false, errors.New("invalid token")
	}

	// Validate standard claims
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return false, errors.New("token expired")
		}
	} else {
		return false, errors.New("missing exp claim")
	}

	return true, nil
}
