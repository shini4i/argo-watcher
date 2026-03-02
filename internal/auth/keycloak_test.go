package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeycloakAuthService_Init verifies that the Keycloak auth service is properly initialized
// with URL, realm, client ID, HTTP client and pre-built userinfo URL.
func TestKeycloakAuthService_Init(t *testing.T) {
	t.Run("should initialize with valid URL", func(t *testing.T) {
		service := &KeycloakAuthService{}

		keycloakURL := "http://localhost:8080/auth"
		realm := "test"
		clientId := "test"

		err := service.Init(keycloakURL, realm, clientId, []string{})

		assert.NoError(t, err)
		assert.Equal(t, keycloakURL, service.Url)
		assert.Equal(t, realm, service.Realm)
		assert.Equal(t, clientId, service.ClientId)
		assert.IsType(t, &http.Client{}, service.client)
		assert.Equal(t, "http://localhost:8080/auth/realms/test/protocol/openid-connect/userinfo", service.userinfoURL)
	})

	t.Run("should return error for invalid URL scheme", func(t *testing.T) {
		service := &KeycloakAuthService{}

		err := service.Init("ftp://localhost:8080", "test", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid keycloak URL scheme")
	})

	t.Run("should return error for missing host", func(t *testing.T) {
		service := &KeycloakAuthService{}

		err := service.Init("http:///auth", "test", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing host")
	})

	t.Run("should return error for URL with query parameters", func(t *testing.T) {
		service := &KeycloakAuthService{}

		err := service.Init("https://kc.example.com?x=1", "test", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query and fragment are not allowed")
	})

	t.Run("should return error for URL with fragment", func(t *testing.T) {
		service := &KeycloakAuthService{}

		err := service.Init("https://kc.example.com#frag", "test", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query and fragment are not allowed")
	})
}

// TestKeycloakAuthService_Validate tests token validation scenarios including
// URL escaping, privileged group membership, and invalid tokens.
func TestKeycloakAuthService_Validate(t *testing.T) {
	t.Run("should escape realm name in URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			// Realm with special chars should be escaped in the path
			// Use EscapedPath to verify percent-encoding is preserved
			assert.Equal(t, "/realms/my%2Frealm/protocol/openid-connect/userinfo", req.URL.EscapedPath())
			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write([]byte(`{"groups": ["group1"]}`))
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		service := &KeycloakAuthService{}
		err := service.Init(server.URL, "my/realm", "test", []string{"group1"})
		assert.NoError(t, err)
		service.client = server.Client()

		ok, err := service.Validate("test")

		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("should return true if token is valid and user is in privileged group", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/realms/test/protocol/openid-connect/userinfo")
			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write([]byte(`{"groups": ["group1"]}`))
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		service := &KeycloakAuthService{}
		err := service.Init(server.URL, "test", "test", []string{"group1"})
		assert.NoError(t, err)
		service.client = server.Client()

		ok, err := service.Validate("test")

		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("should return false if token is valid but user is not in privileged group", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/realms/test/protocol/openid-connect/userinfo")
			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write([]byte(`{"groups": ["group2"]}`))
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		service := &KeycloakAuthService{}
		err := service.Init(server.URL, "test", "test", []string{"group1"})
		assert.NoError(t, err)
		service.client = server.Client()

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.False(t, ok)
	})

	t.Run("should return false if token is invalid", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			assert.Equal(t, req.URL.String(), "/realms/test/protocol/openid-connect/userinfo")
			rw.WriteHeader(http.StatusUnauthorized)
			_, err := rw.Write([]byte(`Unauthorized`))
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		service := &KeycloakAuthService{}
		err := service.Init(server.URL, "test", "test", []string{})
		assert.NoError(t, err)
		service.client = server.Client()

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.False(t, ok)
	})

	t.Run("should return error if response body is invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write([]byte(`invalid json`))
			if err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		service := &KeycloakAuthService{}
		err := service.Init(server.URL, "test", "test", []string{"group1"})
		assert.NoError(t, err)
		service.client = server.Client()

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error unmarshalling response body")
		assert.False(t, ok)
	})

	t.Run("should return error if server is unreachable", func(t *testing.T) {
		service := &KeycloakAuthService{}
		err := service.Init("http://127.0.0.1:1", "test", "test", []string{"group1"})
		assert.NoError(t, err)

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error on response")
		assert.False(t, ok)
	})
}
