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

	// Valid JWT
	t.Run("valid JWT", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.NoError(t, err)
		assert.True(t, isValid)
	})

	// Empty token
	t.Run("empty token", func(t *testing.T) {
		isValid, err := service.Validate("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty token")
		assert.False(t, isValid)
	})

	// Malformed token
	t.Run("malformed token", func(t *testing.T) {
		isValid, err := service.Validate("invalid.token.format")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token is malformed")
		assert.False(t, isValid)
	})

	// Invalid token claims
	t.Run("invalid token claims", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": nil}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token has invalid claims")
		assert.False(t, isValid)
	})

	// Missing exp claim
	t.Run("missing exp claim", func(t *testing.T) {
		claims := jwt.MapClaims{}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing exp claim")
		assert.False(t, isValid)
	})

	// Invalid exp claim type
	t.Run("invalid exp claim type", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": "invalid_type"}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token has invalid claims")
		assert.False(t, isValid)
	})

	// Expired JWT
	t.Run("expired JWT", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(-time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token has invalid claims")
		assert.False(t, isValid)
	})

	// Invalid signing method
	t.Run("invalid signing method", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected signing method")
		assert.False(t, isValid)
	})

	// Token with invalid signature
	t.Run("token with invalid signature", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte("wrong_secret"))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "signature is invalid")
		assert.False(t, isValid)
	})
}
