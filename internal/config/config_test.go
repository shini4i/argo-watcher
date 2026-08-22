package config

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServerConfig(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "postgres")

		cfg, err := NewServerConfig()

		assert.NoError(t, err)
		assert.NotNil(t, cfg)

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

// The database DSN carries a connect_timeout so an unreachable Postgres fails fast at
// startup instead of blocking on the OS TCP timeout.
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

// An explicitly supplied DB_DSN bypasses the default template, so it still gets a
// connect_timeout — while an operator-provided one is left untouched.
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

// A non-positive DB_CONNECT_TIMEOUT is rejected: 0 (and negatives on libpq) mean
// "wait indefinitely" and would silently defeat the fail-fast guard.
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
	// STATE_TYPE is intentionally not asserted: the project's Taskfile sets
	// STATE_TYPE=in-memory for `task test` runs.
	assert.Contains(t, err.Error(), "missing required environment variables:")
	assert.Contains(t, err.Error(), "ARGO_TOKEN")
}

func TestNewServerConfig_InvalidStateType_IsReadable(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "invalid")

	_, err := NewServerConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "StateType")
	assert.Contains(t, err.Error(), "must be one of [postgres in-memory]")
	assert.Contains(t, err.Error(), `"invalid"`)
	assert.NotContains(t, err.Error(), "Key: 'ServerConfig.StateType'")
}

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

// A required variable that is present but empty is rejected at parse time (the
// `,notEmpty` tag), not silently accepted and left to fail later.
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
	config := &ServerConfig{
		DeploymentTimeout: 60,
	}

	retryAttempts := config.GetRetryAttempts()

	assert.Equal(t, uint(5), retryAttempts)
}

func TestNewServerConfig_ArgoApiRetriesDefault(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")

	cfg, err := NewServerConfig()
	assert.NoError(t, err)
	assert.Equal(t, uint(3), cfg.ArgoApiRetries)
}

func TestNewServerConfig_ArgoApiRetriesCustom(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "5")

	cfg, err := NewServerConfig()
	assert.NoError(t, err)
	assert.Equal(t, uint(5), cfg.ArgoApiRetries)
}

// Zero attempts would cause infinite retries with retry-go.
func TestNewServerConfig_ArgoApiRetriesZeroRejected(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "0")

	_, err := NewServerConfig()
	assert.Error(t, err)
}

// 10 is the maximum.
func TestNewServerConfig_ArgoApiRetriesTooHighRejected(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "postgres")
	t.Setenv("ARGO_API_RETRIES", "11")

	_, err := NewServerConfig()
	assert.Error(t, err)
}

// The deprecated KEYCLOAK_* variables map onto the generic OIDC config, with the issuer
// synthesized from KEYCLOAK_URL + KEYCLOAK_REALM, so existing Keycloak deployments keep
// working after the rename without any config change.
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

// The per-field fallback a real upgrade hits: an operator sets the new OIDC_ISSUER_URL
// but still relies on the deprecated KEYCLOAK_CLIENT_ID / KEYCLOAK_PRIVILEGED_GROUPS.
// Each field must resolve independently.
func TestNewServerConfig_OIDCMixedFallback(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", "https://authentik.example.com/application/o/aw/")
	t.Setenv("KEYCLOAK_CLIENT_ID", "legacy-client")
	t.Setenv("KEYCLOAK_PRIVILEGED_GROUPS", "admins,operators")

	cfg, err := NewServerConfig()

	require.NoError(t, err)
	assert.Equal(t, "https://authentik.example.com/application/o/aw/", cfg.OIDC.IssuerURL)
	assert.Equal(t, "legacy-client", cfg.OIDC.ClientId)
	assert.Equal(t, []string{"admins", "operators"}, cfg.OIDC.PrivilegedGroups)
}

// A half-configured legacy Keycloak (URL without realm) must not synthesize a
// malformed issuer.
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

// A malformed deprecated value must fail startup rather than silently disable auth: a typo like
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

func TestNewServerConfig_OIDCPrecedence(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", "https://authentik.example.com/application/o/argo-watcher/")
	t.Setenv("OIDC_CLIENT_ID", "aw-oidc")
	t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
	t.Setenv("KEYCLOAK_REALM", "legacy")
	t.Setenv("KEYCLOAK_CLIENT_ID", "legacy-client")

	cfg, err := NewServerConfig()

	require.NoError(t, err)
	assert.Equal(t, "https://authentik.example.com/application/o/argo-watcher/", cfg.OIDC.IssuerURL)
	assert.Equal(t, "aw-oidc", cfg.OIDC.ClientId)
}

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

// The switch that closes GET /api/v1/tasks/{id} only has meaning while reads are protected at all,
// so setting it with OIDC disabled is a misconfiguration the operator must see: it
// would otherwise leave the endpoint open while reading as if it were closed.
func TestNewServerConfig_RequireTaskReadAuth(t *testing.T) {
	baseEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "in-memory")
	}

	t.Run("defaults to off", func(t *testing.T) {
		baseEnv(t)

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.False(t, cfg.OIDC.RequireTaskReadAuth)
	})

	t.Run("accepted with OIDC enabled", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("OIDC_ENABLED", "true")
		t.Setenv("OIDC_ISSUER_URL", "https://idp.example.com/application/o/aw/")
		t.Setenv("OIDC_CLIENT_ID", "argo-watcher")
		t.Setenv("OIDC_REQUIRE_TASK_READ_AUTH", "true")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.True(t, cfg.OIDC.RequireTaskReadAuth)
	})

	t.Run("rejected with OIDC disabled", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("OIDC_REQUIRE_TASK_READ_AUTH", "true")

		_, err := NewServerConfig()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC.RequireTaskReadAuth")
		assert.Contains(t, err.Error(), "OIDC_ENABLED")
	})

	t.Run("accepted when OIDC is enabled through the legacy KEYCLOAK_* variables", func(t *testing.T) {
		// The guard reads OIDC.Enabled, which a legacy deployment only sets via
		// applyKeycloakCompat. Checking it before that mapping would refuse to start
		// every Keycloak-configured server, naming a variable its operator never set.
		baseEnv(t)
		t.Setenv("KEYCLOAK_ENABLED", "true")
		t.Setenv("KEYCLOAK_URL", "https://kc.example.com")
		t.Setenv("KEYCLOAK_REALM", "argo-watcher")
		t.Setenv("KEYCLOAK_CLIENT_ID", "argo-watcher")
		t.Setenv("OIDC_REQUIRE_TASK_READ_AUTH", "true")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.True(t, cfg.OIDC.RequireTaskReadAuth)
	})
}

// /api/v1/config exposes the OIDC block under both the canonical "oidc" key and the
// legacy "keycloak" mirror, preserving backward compatibility for old consumers.
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

	// /api/v1/config is readable without a credential, and read policy is the
	// server's business, not bootstrap data any client needs.
	cfg.OIDC.RequireTaskReadAuth = true
	withPolicy, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(withPolicy), "require_task_read_auth")
}

func TestServerConfig_JSONExcludesSensitiveFields(t *testing.T) {
	databaseConfig := DatabaseConfig{}
	config := &ServerConfig{
		ArgoToken:   "secret-token",
		DeployToken: "deploy-token",
		Db:          databaseConfig,
	}

	jsonBytes, err := json.Marshal(config)
	assert.NoError(t, err)

	jsonString := string(jsonBytes)

	assert.NotContains(t, jsonString, "secret-token")
	assert.NotContains(t, jsonString, "db-password")
	assert.NotContains(t, jsonString, "deploy-token")
}

// GET /api/v1/config is unauthenticated: how to reach a notification receiver must not be
// readable by anyone who can reach the server, since a webhook URL is itself the credential.
// The `enabled` flags stay — they name no target, and argo-watcher-mcp forwards them.
func TestServerConfig_JSONOmitsNotificationTargets(t *testing.T) {
	config := &ServerConfig{
		SkipTlsVerify:    true,
		LockdownSchedule: "Mon-Fri 09:00-18:00",
		Webhook: WebhookConfig{
			Enabled: true,
			Url:     "https://hooks.example.com/services/T000/B000/XXXX",
			Token:   "webhook-token",
		},
		Mattermost: MattermostConfig{
			Enabled:   true,
			Url:       "https://mattermost.example.com",
			ChannelId: "channel-id",
			Token:     "mattermost-token",
		},
	}

	jsonBytes, err := json.Marshal(config)
	require.NoError(t, err)
	jsonString := string(jsonBytes)

	assert.NotContains(t, jsonString, "hooks.example.com")
	assert.NotContains(t, jsonString, "webhook-token")
	assert.NotContains(t, jsonString, "mattermost.example.com")
	assert.NotContains(t, jsonString, "mattermost-token")
	assert.NotContains(t, jsonString, "channel-id")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &decoded))
	assert.Equal(t, true, decoded["webhook"].(map[string]any)["enabled"])
	assert.Equal(t, true, decoded["mattermost"].(map[string]any)["enabled"])
	assert.Equal(t, "Mon-Fri 09:00-18:00", decoded["lockdown_schedule"])
	assert.Equal(t, true, decoded["skip_tls_verify"])
	assert.Contains(t, jsonString, "argo_cd_url")
	assert.Contains(t, jsonString, "oidc")
}

// Revalidating every 10s would turn every UI refresh into provider traffic.
func TestServerConfig_DefaultValidationInterval(t *testing.T) {
	t.Setenv("ARGO_URL", "https://example.com")
	t.Setenv("ARGO_TOKEN", "secret-token")
	t.Setenv("STATE_TYPE", "in-memory")

	cfg, err := NewServerConfig()

	require.NoError(t, err)
	assert.Equal(t, 300000, cfg.OIDC.TokenValidationInterval)
}

func TestNewServerConfig_TaskRetention(t *testing.T) {
	baseEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "postgres")
	}

	t.Run("defaults to off with a one year window", func(t *testing.T) {
		baseEnv(t)

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.False(t, cfg.TaskRetentionEnabled)
		assert.Equal(t, 365, cfg.TaskRetentionDays)
	})

	t.Run("custom window accepted", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("TASK_RETENTION_ENABLED", "true")
		t.Setenv("TASK_RETENTION_DAYS", "30")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.True(t, cfg.TaskRetentionEnabled)
		assert.Equal(t, 30, cfg.TaskRetentionDays)
	})

	// A zero or negative window would put the cutoff at or after now, deleting the
	// entire deployment history on the next sweep.
	t.Run("non-positive window rejected when enabled", func(t *testing.T) {
		for _, days := range []string{"0", "-1"} {
			t.Run(days, func(t *testing.T) {
				baseEnv(t)
				t.Setenv("TASK_RETENTION_ENABLED", "true")
				t.Setenv("TASK_RETENTION_DAYS", days)

				_, err := NewServerConfig()

				require.Error(t, err)
				assert.Contains(t, err.Error(), "TaskRetentionDays")
				assert.Contains(t, err.Error(), "must be between 1 and 36500 days")
			})
		}
	})

	// A window long enough to push the cutoff outside the range Postgres can
	// represent fails inside the hourly sweep, where it names no setting.
	t.Run("window beyond a century rejected when enabled", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("TASK_RETENTION_ENABLED", "true")
		t.Setenv("TASK_RETENTION_DAYS", "36501")

		_, err := NewServerConfig()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "TaskRetentionDays")
		assert.Contains(t, err.Error(), "got 36501")
	})

	t.Run("a century is accepted", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("TASK_RETENTION_ENABLED", "true")
		t.Setenv("TASK_RETENTION_DAYS", "36500")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.Equal(t, 36500, cfg.TaskRetentionDays)
	})

	// With the toggle off nothing is ever deleted, so the window is inert and must
	// not keep a server from starting.
	t.Run("non-positive window ignored when disabled", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("TASK_RETENTION_DAYS", "0")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.False(t, cfg.TaskRetentionEnabled)
	})

	// In-memory tasks die with the process, so retention has nothing to do there.
	// It is inert rather than contradictory, so it warns instead of failing startup.
	t.Run("accepted with in-memory state", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("STATE_TYPE", "in-memory")
		t.Setenv("TASK_RETENTION_ENABLED", "true")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.True(t, cfg.TaskRetentionEnabled)
	})
}

// The retention settings are server-side only: /api/v1/config is unauthenticated
// and no consumer reads them. Dropping the json:"-" tags would expose them under
// a Go-derived key such as TaskRetentionDays, so the check is by key substring
// and by value rather than by the snake_case name they would never carry.
func TestServerConfig_TaskRetentionNotExposed(t *testing.T) {
	cfg := &ServerConfig{TaskRetentionEnabled: true, TaskRetentionDays: 4242}

	jsonBytes, err := json.Marshal(cfg)
	require.NoError(t, err)

	assert.NotContains(t, strings.ToLower(string(jsonBytes)), "retention")
	assert.NotContains(t, string(jsonBytes), "4242", "the window must not leak under a renamed key either")
}

// The Gravatar fallback sends a hash of the signed-in user's email to a third party,
// so it must stay off unless an operator turns it on, and it must reach the UI through
// /api/v1/config for the browser to act on it.
func TestNewServerConfig_GravatarFallback(t *testing.T) {
	baseEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("ARGO_URL", "https://example.com")
		t.Setenv("ARGO_TOKEN", "secret-token")
		t.Setenv("STATE_TYPE", "in-memory")
	}

	t.Run("defaults to off", func(t *testing.T) {
		baseEnv(t)

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.False(t, cfg.OIDC.GravatarFallback)
	})

	t.Run("enabled by the operator", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("OIDC_ENABLED", "true")
		t.Setenv("OIDC_ISSUER_URL", "https://idp.example.com/application/o/aw/")
		t.Setenv("OIDC_CLIENT_ID", "argo-watcher")
		t.Setenv("OIDC_GRAVATAR_FALLBACK", "true")

		cfg, err := NewServerConfig()

		require.NoError(t, err)
		assert.True(t, cfg.OIDC.GravatarFallback)
	})

	t.Run("is published to the UI", func(t *testing.T) {
		cfg := &ServerConfig{OIDC: OIDCConfig{Enabled: true, GravatarFallback: true}}

		encoded, err := json.Marshal(cfg)

		require.NoError(t, err)
		assert.Contains(t, string(encoded), `"gravatar_fallback":true`)
	})
}
