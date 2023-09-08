package config

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServerConfig(t *testing.T) {
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
	// Create a ServerConfig instance with a specific ArgoTimeout value
	config := &ServerConfig{
		ArgoTimeout: 60,
	}

	// Call the GetRetryAttempts function
	retryAttempts := config.GetRetryAttempts()

	// Assert that the retryAttempts value matches the expected result
	assert.Equal(t, uint(5), retryAttempts)
}
