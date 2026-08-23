package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTAuthService(t *testing.T) {
	secretKey := "test_secret_key"
	service := NewJWTAuthService(secretKey, "", "")

	t.Run("valid JWT", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.NoError(t, err)
		assert.True(t, isValid)
	})

	t.Run("empty token", func(t *testing.T) {
		isValid, err := service.Validate("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty token")
		assert.False(t, isValid)
	})

	t.Run("malformed token", func(t *testing.T) {
		isValid, err := service.Validate("invalid.token.format")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token is malformed")
		assert.False(t, isValid)
	})

	t.Run("completely invalid token", func(t *testing.T) {
		isValid, err := service.Validate("randomgarbage123")
		assert.Error(t, err)
		assert.False(t, isValid)
	})

	t.Run("missing exp claim", func(t *testing.T) {
		claims := jwt.MapClaims{}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exp claim is required")
		assert.False(t, isValid)
	})

	t.Run("expired JWT", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(-time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token has invalid claims: token is expired")
		assert.False(t, isValid)
	})

	t.Run("invalid signing method", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected signing method")
		assert.False(t, isValid)
	})

	t.Run("token with invalid signature", func(t *testing.T) {
		claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte("wrong_secret"))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "signature is invalid")
		assert.False(t, isValid)
	})

	t.Run("token used before issued", func(t *testing.T) {
		claims := jwt.MapClaims{
			"exp": float64(time.Now().Add(time.Hour).Unix()),
			"iat": float64(time.Now().Add(time.Hour).Unix()),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token used before issued")
		assert.False(t, isValid)
	})

	t.Run("token used before allowed time", func(t *testing.T) {
		claims := jwt.MapClaims{
			"exp": float64(time.Now().Add(time.Hour).Unix()),
			"iat": float64(time.Now().Unix()),
			"nbf": float64(time.Now().Add(time.Hour).Unix()),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte(secretKey))

		isValid, err := service.Validate(tokenStr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token is not valid yet")
		assert.False(t, isValid)
	})
}

// TestJWTAuthService_ClaimBinding covers the optional iss/aud binding. The checks are
// strict once configured: a token missing the claim is rejected, so an operator must
// roll the new claims out to every pipeline before setting the variables.
func TestJWTAuthService_ClaimBinding(t *testing.T) {
	secretKey := "test_secret_key"

	sign := func(t *testing.T, claims jwt.MapClaims) string {
		t.Helper()
		claims["exp"] = float64(time.Now().Add(time.Hour).Unix())
		tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secretKey))
		require.NoError(t, err)
		return tokenStr
	}

	t.Run("unconfigured service ignores iss and aud", func(t *testing.T) {
		service := NewJWTAuthService(secretKey, "", "")

		isValid, err := service.Validate(sign(t, jwt.MapClaims{"iss": "https://gitlab.com", "aud": "someone-else"}))

		assert.NoError(t, err)
		assert.True(t, isValid)
	})

	t.Run("issuer must match when configured", func(t *testing.T) {
		service := NewJWTAuthService(secretKey, "https://ci.example.com", "")

		isValid, err := service.Validate(sign(t, jwt.MapClaims{"iss": "https://ci.example.com"}))
		assert.NoError(t, err)
		assert.True(t, isValid)

		isValid, err = service.Validate(sign(t, jwt.MapClaims{"iss": "https://gitlab.com"}))
		assert.ErrorIs(t, err, jwt.ErrTokenInvalidIssuer)
		assert.False(t, isValid)
	})

	t.Run("a token without iss is rejected once an issuer is configured", func(t *testing.T) {
		service := NewJWTAuthService(secretKey, "https://ci.example.com", "")

		isValid, err := service.Validate(sign(t, jwt.MapClaims{}))

		assert.ErrorIs(t, err, jwt.ErrTokenRequiredClaimMissing)
		assert.False(t, isValid)
	})

	t.Run("audience must match when configured", func(t *testing.T) {
		service := NewJWTAuthService(secretKey, "", "argo-watcher")

		isValid, err := service.Validate(sign(t, jwt.MapClaims{"aud": "argo-watcher"}))
		assert.NoError(t, err)
		assert.True(t, isValid)

		// A list carrying the expected value among others is a match.
		isValid, err = service.Validate(sign(t, jwt.MapClaims{"aud": []string{"other", "argo-watcher"}}))
		assert.NoError(t, err)
		assert.True(t, isValid)

		isValid, err = service.Validate(sign(t, jwt.MapClaims{"aud": "other-system"}))
		assert.ErrorIs(t, err, jwt.ErrTokenInvalidAudience)
		assert.False(t, isValid)
	})

	t.Run("a token without aud is rejected once an audience is configured", func(t *testing.T) {
		service := NewJWTAuthService(secretKey, "", "argo-watcher")

		isValid, err := service.Validate(sign(t, jwt.MapClaims{}))

		assert.ErrorIs(t, err, jwt.ErrTokenRequiredClaimMissing)
		assert.False(t, isValid)
	})

	t.Run("both claims are enforced together", func(t *testing.T) {
		service := NewJWTAuthService(secretKey, "https://ci.example.com", "argo-watcher")

		isValid, err := service.Validate(sign(t, jwt.MapClaims{"iss": "https://ci.example.com", "aud": "argo-watcher"}))
		assert.NoError(t, err)
		assert.True(t, isValid)

		isValid, err = service.Validate(sign(t, jwt.MapClaims{"iss": "https://ci.example.com"}))
		assert.Error(t, err)
		assert.False(t, isValid)
	})
}
