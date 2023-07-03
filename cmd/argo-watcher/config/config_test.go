package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServerConfig(t *testing.T) {
	// Set up the required environment variables
	if err := os.Setenv("ARGO_URL", "https://example.com"); err != nil {
		t.Fatal(err)
	}

	if err := os.Setenv("ARGO_TOKEN", "secret-token"); err != nil {
		t.Fatal(err)
	}

	if err := os.Setenv("STATE_TYPE", "postgres"); err != nil {
		t.Fatal(err)
	}

	// Cleanup the environment variables after the test
	defer func() {
		if err := os.Unsetenv("ARGO_URL"); err != nil {
			t.Fatal(err)
		}

		if err := os.Unsetenv("ARGO_TOKEN"); err != nil {
			t.Fatal(err)
		}

		if err := os.Unsetenv("STATE_TYPE"); err != nil {
			t.Fatal(err)
		}
	}()

	// Call the NewServerConfig function
	cfg, err := NewServerConfig()

	// Assert that the configuration was parsed successfully
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// Assert specific field values
	assert.Equal(t, "https://example.com", cfg.ArgoUrl)
	assert.Equal(t, "secret-token", cfg.ArgoToken)
	assert.Equal(t, "postgres", cfg.StateType)
}

func TestNewServerConfig_RequiredFieldsMissing(t *testing.T) {
	// Set up environment variables with missing required fields
	if err := os.Setenv("ARGO_URL", "https://example.com"); err != nil {
		t.Fatal(err)
	}

	// Cleanup the environment variables after the test
	defer func() {
		if err := os.Unsetenv("ARGO_URL"); err != nil {
			t.Fatal(err)
		}
	}()

	// Call the NewServerConfig function
	cfg, err := NewServerConfig()

	// Assert that an error is returned due to missing required fields
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestServerConfig_GetRetryAttempts(t *testing.T) {
	// Create a ServerConfig instance with a specific ArgoTimeout value
	config := &ServerConfig{
		ArgoTimeout: "60",
	}

	// Call the GetRetryAttempts function
	retryAttempts := config.GetRetryAttempts()

	// Assert that the retryAttempts value matches the expected result
	assert.Equal(t, uint(5), retryAttempts)
}
