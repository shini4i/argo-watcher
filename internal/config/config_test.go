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

	cfg, err := NewServerConfig()

	assert.Error(t, err)
	assert.Nil(t, cfg)
	// Assert the formatter is wired in: the message must use the grouped
	// header, and ARGO_TOKEN (which this test never sets) must appear under
	// it. STATE_TYPE is intentionally not asserted because the project's
	// Taskfile sets STATE_TYPE=in-memory for `task test` runs.
	assert.Contains(t, err.Error(), "missing required environment variables:")
	assert.Contains(t, err.Error(), "ARGO_TOKEN")
}

// TestNewServerConfig_InvalidStateType_IsReadable verifies that the
// validator error names the field, the constraint, and the offending value
// — replacing the unreadable
// "Key: 'ServerConfig.StateType' Error:Field validation for ... 'oneof' tag"
// blob with something an operator can act on directly.
func TestNewServerConfig_InvalidStateType_IsReadable(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "invalid")

	_, err := NewServerConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "StateType")
	assert.Contains(t, err.Error(), "must be one of [postgres in-memory]")
	assert.Contains(t, err.Error(), `"invalid"`)
	// We no longer leak go-playground/validator's blob format.
	assert.NotContains(t, err.Error(), "Key: 'ServerConfig.StateType'")
}

// TestNewServerConfig_InvalidArgoApiRetries_IsReadable verifies the same for
// numeric range validation.
func TestNewServerConfig_InvalidArgoApiRetries_IsReadable(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "11")

	_, err := NewServerConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ArgoApiRetries")
	assert.Contains(t, err.Error(), "must be between 1 and 10")
	assert.Contains(t, err.Error(), "got 11")
}

// TestNewServerConfig_EmptyRequiredRejected verifies that a required variable
// that is present but empty is rejected at parse time (the `,notEmpty` tag),
// not silently accepted and left to fail later. This guards the empty-value
// rejection that replaced go-playground/validator.
func TestNewServerConfig_EmptyRequiredRejected(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("ARGO_TOKEN", "") // set, but empty

	_, err := NewServerConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ARGO_TOKEN")
	assert.Contains(t, err.Error(), "should not be empty")
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
