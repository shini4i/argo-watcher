package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeycloakAuthService_Init(t *testing.T) {
	service := &KeycloakAuthService{}

	url := "http://localhost:8080/auth"
	realm := "test"
	clientId := "test"

	service.Init(url, realm, clientId, []string{})

	assert.Equal(t, url, service.Url)
	assert.Equal(t, realm, service.Realm)
	assert.Equal(t, clientId, service.ClientId)
	assert.IsType(t, &http.Client{}, service.client)
}

func TestKeycloakAuthService_Validate(t *testing.T) {
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

		service := &KeycloakAuthService{
			Url:              server.URL,
			Realm:            "test",
			ClientId:         "test",
			PrivilegedGroups: []string{"group1"},
			client:           server.Client(),
		}

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

		service := &KeycloakAuthService{
			Url:              server.URL,
			Realm:            "test",
			ClientId:         "test",
			PrivilegedGroups: []string{"group1"},
			client:           server.Client(),
		}

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

		service := &KeycloakAuthService{
			Url:      server.URL,
			Realm:    "test",
			ClientId: "test",
			client:   server.Client(),
		}

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.False(t, ok)
	})
}
