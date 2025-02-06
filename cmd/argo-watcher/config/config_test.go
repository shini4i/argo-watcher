package config

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServerConfig(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Set up the required environment variables
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "postgres")

		// Call the NewServerConfig function
		cfg, err := NewServerConfig()

		// Assert that the configuration was parsed successfully
		assert.NoError(t, err)
		assert.NotNil(t, cfg)

		// Assert specific field values
		expectedUrl, _ := url.Parse("https://example.com")
		assert.Equal(t, *expectedUrl, cfg.ArgoUrl)
		assert.Equal(t, "secret-token", cfg.ArgoToken)
		assert.Equal(t, "postgres", cfg.StateType)
	})

	t.Run("Invalid state type", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "invalid")

		_, err := NewServerConfig()
		assert.Error(t, err)
	})
}

func TestNewServerConfig_RequiredFieldsMissing(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")

	// Call the NewServerConfig function
	cfg, err := NewServerConfig()

	// Assert that an error is returned due to missing required fields
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestServerConfig_GetRetryAttempts(t *testing.T) {
	// Create a ServerConfig instance with a specific DeploymentTimeout value
	config := &ServerConfig{
		DeploymentTimeout: 60,
	}

	// Call the GetRetryAttempts function
	retryAttempts := config.GetRetryAttempts()

	// Assert that the retryAttempts value matches the expected result
	assert.Equal(t, uint(5), retryAttempts)
}

func TestServerConfig_JSONExcludesSensitiveFields(t *testing.T) {
	databaseConfig := DatabaseConfig{}
	// Create a ServerConfig instance with some dummy data
	config := &ServerConfig{
		ArgoToken:   "secret-token",
		DeployToken: "deploy-token",
		Db:          databaseConfig,
	}

	// Marshal the ServerConfig instance to JSON
	jsonBytes, err := json.Marshal(config)
	assert.NoError(t, err)

	// Convert the JSON bytes to a string
	jsonString := string(jsonBytes)

	// Check that the sensitive fields are not present in the JSON string
	assert.NotContains(t, jsonString, "secret-token")
	assert.NotContains(t, jsonString, "db-password")
	assert.NotContains(t, jsonString, "deploy-token")
}
