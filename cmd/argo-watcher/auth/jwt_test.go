package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestJWTAuthService(t *testing.T) {
	secretKey := "test_secret_key"
	service := &JWTAuthService{}
	service.Init(secretKey)

	t.Run("valid JWT", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "test_issuer",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

		tokenStr, err := token.SignedString([]byte(secretKey))
		assert.NoError(t, err)

		isValid, err := service.Validate(tokenStr)
		assert.NoError(t, err)
		assert.True(t, isValid)
	})

	t.Run("expired JWT", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			Issuer:    "test_issuer",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

		tokenStr, err := token.SignedString([]byte(secretKey))
		assert.NoError(t, err)

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.False(t, isValid)
	})

	t.Run("Invalid JWT", func(t *testing.T) {
		tokenStr := "invalid_token"

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.False(t, isValid)
	})
}
