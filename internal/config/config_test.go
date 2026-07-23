package config

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestNewServerConfig_DatabaseConnectTimeout verifies that the database DSN
// carries a connect_timeout so an unreachable Postgres fails fast at startup
// instead of blocking on the OS TCP timeout, and that DB_CONNECT_TIMEOUT
// overrides the default.
func TestNewServerConfig_DatabaseConnectTimeout(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "postgres")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.Equal(t, 10, cfg.Db.ConnectTimeout)
		assert.Contains(t, cfg.Db.DSN, "connect_timeout=10")
	})

	t.Run("Override", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "postgres")
		t.Setenv("DB_CONNECT_TIMEOUT", "3")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.Equal(t, 3, cfg.Db.ConnectTimeout)
		assert.Contains(t, cfg.Db.DSN, "connect_timeout=3")
	})
}

// TestNewServerConfig_ConnectTimeoutValidation verifies that a non-positive
// DB_CONNECT_TIMEOUT is rejected with a readable error, since 0 (and negatives on
// libpq) mean "wait indefinitely" and would silently defeat the fail-fast guard.
// TestNewServerConfig_ConnectTimeoutInjectedIntoCustomDSN verifies that an
// explicitly supplied DB_DSN (which bypasses the default template) still gets a
// connect_timeout so the fail-fast guard cannot be silently defeated, while an
// operator-provided connect_timeout is left untouched.
func TestNewServerConfig_ConnectTimeoutInjectedIntoCustomDSN(t *testing.T) {
	base := func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "postgres")
	}

	t.Run("Keyword/value DSN without connect_timeout", func(t *testing.T) {
		base(t)
		t.Setenv("DB_DSN", "host=db port=5432 user=u password=p dbname=aw sslmode=disable")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.Equal(t, "host=db port=5432 user=u password=p dbname=aw sslmode=disable connect_timeout=10", cfg.Db.DSN)
	})

	t.Run("URI DSN without connect_timeout", func(t *testing.T) {
		base(t)
		t.Setenv("DB_DSN", "postgres://db:5432/aw?sslmode=disable")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.Equal(t, "postgres://db:5432/aw?sslmode=disable&connect_timeout=10", cfg.Db.DSN)
	})

	t.Run("Operator connect_timeout is respected", func(t *testing.T) {
		base(t)
		t.Setenv("DB_CONNECT_TIMEOUT", "10")
		t.Setenv("DB_DSN", "host=db user=u connect_timeout=30")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.Equal(t, "host=db user=u connect_timeout=30", cfg.Db.DSN)
	})
}

func TestNewServerConfig_ConnectTimeoutValidation(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ARGO_URL", "https://example.com")
			t.Setenv("ARGO_TOKEN", "secret-token")
			t.Setenv("STATE_TYPE", "postgres")
			t.Setenv("DB_CONNECT_TIMEOUT", value)

			cfg, err := NewServerConfig()

			assert.Nil(t, cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "ConnectTimeout")
			assert.Contains(t, err.Error(), "must be at least 1 second")
		})
	}
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

// TestNewServerConfig_OIDCKeycloakFallback verifies that the deprecated
// KEYCLOAK_* variables are mapped onto the generic OIDC config, with the issuer
// synthesized from KEYCLOAK_URL + KEYCLOAK_REALM, so existing Keycloak
// deployments keep working after the rename without any config change.
func TestNewServerConfig_OIDCKeycloakFallback(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("KEYCLOAK_ENABLED", "true")
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com/")
	t.Setenv("KEYCLOAK_REALM", "argo-watcher")
	t.Setenv("KEYCLOAK_CLIENT_ID", "argo-watcher")
	t.Setenv("KEYCLOAK_TOKEN_VALIDATION_INTERVAL", "5000")
	t.Setenv("KEYCLOAK_PRIVILEGED_GROUPS", "admins, operators")

	cfg, err := NewServerConfig()

	require.NoError(t, err)
	assert.True(t, cfg.OIDC.Enabled)
	assert.Equal(t, "https://kc.example.com/realms/argo-watcher", cfg.OIDC.IssuerURL)
	assert.Equal(t, "argo-watcher", cfg.OIDC.ClientId)
	assert.Equal(t, 5000, cfg.OIDC.TokenValidationInterval)
	assert.Equal(t, []string{"admins", "operators"}, cfg.OIDC.PrivilegedGroups)
}

// TestNewServerConfig_OIDCMixedFallback verifies the per-field fallback that a
// real upgrade hits: an operator sets the new OIDC_ISSUER_URL but still relies on
// the deprecated KEYCLOAK_CLIENT_ID / KEYCLOAK_PRIVILEGED_GROUPS. Each field must
// resolve independently — the OIDC issuer plus the Keycloak-sourced client id and
// groups.
func TestNewServerConfig_OIDCMixedFallback(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", "https://authentik.example.com/application/o/aw/")
	// Deprecated aliases still supply the fields OIDC_* leaves unset.
	t.Setenv("KEYCLOAK_CLIENT_ID", "legacy-client")
	t.Setenv("KEYCLOAK_PRIVILEGED_GROUPS", "admins,operators")

	cfg, err := NewServerConfig()

	require.NoError(t, err)
	assert.Equal(t, "https://authentik.example.com/application/o/aw/", cfg.OIDC.IssuerURL)
	assert.Equal(t, "legacy-client", cfg.OIDC.ClientId)
	assert.Equal(t, []string{"admins", "operators"}, cfg.OIDC.PrivilegedGroups)
}

// TestNewServerConfig_KeycloakPartialIssuer verifies that a half-configured legacy
// Keycloak (URL without realm) does not synthesize a malformed issuer; with auth
// enabled it must surface as the OIDC.IssuerURL validation error.
func TestNewServerConfig_KeycloakPartialIssuer(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("KEYCLOAK_ENABLED", "true")
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
	// KEYCLOAK_REALM intentionally unset.
	t.Setenv("KEYCLOAK_CLIENT_ID", "argo-watcher")

	_, err := NewServerConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OIDC.IssuerURL")
}

// TestNewServerConfig_KeycloakMalformedValuesRejected verifies that a malformed
// deprecated value fails startup rather than silently disabling auth: a typo like
// KEYCLOAK_ENABLED=yes must never quietly drop the protected OIDC routes from an
// otherwise healthy deployment (fail closed, not open).
func TestNewServerConfig_KeycloakMalformedValuesRejected(t *testing.T) {
	t.Run("malformed KEYCLOAK_ENABLED", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "in-memory")
		t.Setenv("KEYCLOAK_ENABLED", "yes")

		_, err := NewServerConfig()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "KEYCLOAK_ENABLED")
	})

	t.Run("malformed KEYCLOAK_TOKEN_VALIDATION_INTERVAL", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "in-memory")
		t.Setenv("KEYCLOAK_ENABLED", "true")
		t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
		t.Setenv("KEYCLOAK_REALM", "demo")
		t.Setenv("KEYCLOAK_CLIENT_ID", "argo-watcher")
		t.Setenv("KEYCLOAK_TOKEN_VALIDATION_INTERVAL", "soon")

		_, err := NewServerConfig()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "KEYCLOAK_TOKEN_VALIDATION_INTERVAL")
	})
}

// TestNewServerConfig_OIDCPrecedence verifies that OIDC_* takes precedence over
// the deprecated KEYCLOAK_* aliases when both are set.
func TestNewServerConfig_OIDCPrecedence(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", "https://authentik.example.com/application/o/argo-watcher/")
	t.Setenv("OIDC_CLIENT_ID", "aw-oidc")
	// Legacy values that must be ignored because OIDC_* is set.
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
	t.Setenv("KEYCLOAK_REALM", "legacy")
	t.Setenv("KEYCLOAK_CLIENT_ID", "legacy-client")

	cfg, err := NewServerConfig()

	require.NoError(t, err)
	assert.Equal(t, "https://authentik.example.com/application/o/argo-watcher/", cfg.OIDC.IssuerURL)
	assert.Equal(t, "aw-oidc", cfg.OIDC.ClientId)
}

// TestNewServerConfig_OIDCValidation verifies that enabling OIDC without an
// issuer or client id is rejected with a readable, field-named error.
func TestNewServerConfig_OIDCValidation(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("OIDC_ENABLED", "true")

	_, err := NewServerConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OIDC.IssuerURL")
	assert.Contains(t, err.Error(), "OIDC.ClientId")
}

// TestServerConfig_JSONDualKey verifies that /api/v1/config exposes the OIDC
// block under both the canonical "oidc" key and the legacy "keycloak" mirror
// with identical content, preserving backward compatibility for old consumers.
func TestServerConfig_JSONDualKey(t *testing.T) {
	cfg := &ServerConfig{
		OIDC: OIDCConfig{
			Enabled:   true,
			IssuerURL: "https://kc.example.com/realms/argo-watcher",
			ClientId:  "argo-watcher",
		},
	}

	jsonBytes, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(jsonBytes, &decoded))

	oidcRaw, hasOIDC := decoded["oidc"]
	kcRaw, hasKeycloak := decoded["keycloak"]
	require.True(t, hasOIDC, "expected an oidc block")
	require.True(t, hasKeycloak, "expected a legacy keycloak mirror block")
	assert.JSONEq(t, string(oidcRaw), string(kcRaw), "keycloak mirror must match the oidc block")
	assert.Contains(t, string(oidcRaw), `"issuer_url":"https://kc.example.com/realms/argo-watcher"`)
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
