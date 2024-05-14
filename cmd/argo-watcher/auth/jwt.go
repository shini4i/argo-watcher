package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuthService is a struct that manages JWT authentication using a secret key.
type JWTAuthService struct {
	// secretKey is the signing key for JWT.
	secretKey string
}

// Init initializes the JWT authentication service with a provided secret key.
func (j *JWTAuthService) Init(secretKey string) {
	j.secretKey = secretKey
}

// Validate verifies the validity of a JWT token string.
// It uses the stored secretKey for verification.
// The function returns a boolean indicating the validity of the token, and any errors encountered during the process.
func (j *JWTAuthService) Validate(tokenStr string) (bool, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(j.secretKey), nil
	})

	if err != nil {
		return false, err
	}

	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		return false, fmt.Errorf("token is not valid")
	}

	return true, nil
}
