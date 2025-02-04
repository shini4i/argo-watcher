package auth

import (
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/stretchr/testify/assert"
)

func TestNewKeycloakAuthService(t *testing.T) {
	conf := &config.ServerConfig{
		Keycloak: config.KeycloakConfig{
			Url:              "http://localhost:8080",
			Realm:            "master",
			ClientId:         "test",
			PrivilegedGroups: []string{"group1", "group2"},
		},
	}

	keycloakAuthService := NewKeycloakAuthService(conf)

	assert.Equal(t, keycloakAuthService.Url, conf.Keycloak.Url)
	assert.Equal(t, keycloakAuthService.Realm, conf.Keycloak.Realm)
	assert.Equal(t, keycloakAuthService.ClientId, conf.Keycloak.ClientId)
	assert.Equal(t, keycloakAuthService.PrivilegedGroups, conf.Keycloak.PrivilegedGroups)
}

func TestNewJWTAuthService(t *testing.T) {
	secret := "testSecret"
	jwtAuthService := NewJWTAuthService(secret)
	assert.Equal(t, jwtAuthService.secretKey, []byte(secret))
}
