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

	t.Run("Tokens with whitespace are trimmed", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "  secret-token\n")
		t.Setenv("ARGO_WATCHER_DEPLOY_TOKEN", "  deploy-token\n")
		t.Setenv("JWT_SECRET", "  jwt-secret\n")
		t.Setenv("STATE_TYPE", "postgres")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.Equal(t, "secret-token", cfg.ArgoToken)
		assert.Equal(t, "deploy-token", cfg.DeployToken)
		assert.Equal(t, "jwt-secret", cfg.JWTSecret)
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

// TestNewServerConfig_ArgoApiRetriesDefault verifies that the ArgoApiRetries field
// defaults to 3 when not explicitly set via environment variable.
func TestNewServerConfig_ArgoApiRetriesDefault(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")

	cfg, err := NewServerConfig()
	assert.NoError(t, err)
	assert.Equal(t, uint(3), cfg.ArgoApiRetries)
}

// TestNewServerConfig_ArgoApiRetriesCustom verifies that the ArgoApiRetries field
// can be overridden via the ARGO_API_RETRIES environment variable.
func TestNewServerConfig_ArgoApiRetriesCustom(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "5")

	cfg, err := NewServerConfig()
	assert.NoError(t, err)
	assert.Equal(t, uint(5), cfg.ArgoApiRetries)
}

// TestNewServerConfig_ArgoApiRetriesZeroRejected verifies that setting ARGO_API_RETRIES=0
// fails validation, since zero attempts would cause infinite retries with retry-go.
func TestNewServerConfig_ArgoApiRetriesZeroRejected(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "0")

	_, err := NewServerConfig()
	assert.Error(t, err)
}

// TestNewServerConfig_ArgoApiRetriesTooHighRejected verifies that setting ARGO_API_RETRIES
// above the maximum (10) fails validation.
func TestNewServerConfig_ArgoApiRetriesTooHighRejected(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "11")

	_, err := NewServerConfig()
	assert.Error(t, err)
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
