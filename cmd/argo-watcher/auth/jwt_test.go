package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestJWTAuthService(t *testing.T) {
	secretKey := "test_secret_key"
	service := &JWTAuthService{secretKey: []byte(secretKey)}

	t.Run("valid JWT", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

		tokenStr, err := token.SignedString([]byte(secretKey))
		assert.NoError(t, err)

		isValid, err := service.Validate(tokenStr)
		assert.NoError(t, err)
		assert.True(t, isValid)
	})

	t.Run("missing exp claim", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{
			Issuer: "test_issuer",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

		tokenStr, err := token.SignedString([]byte(secretKey))
		assert.NoError(t, err)

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.False(t, isValid)
		assert.Contains(t, err.Error(), "missing exp claim")
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
		assert.Contains(t, err.Error(), "token is expired")
		assert.False(t, isValid)
	})

	t.Run("Invalid JWT", func(t *testing.T) {
		tokenStr := "invalid_token"

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.False(t, isValid)
	})

	t.Run("just expired JWT", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Second)),
			Issuer:    "test_issuer",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

		tokenStr, err := token.SignedString([]byte(secretKey))
		assert.NoError(t, err)

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token is expired")
		assert.False(t, isValid)
	})

	t.Run("invalid signing method", func(t *testing.T) {
		claims := &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		tokenStr, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
		assert.NoError(t, err)

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected signing method")
		assert.False(t, isValid)
	})
}
