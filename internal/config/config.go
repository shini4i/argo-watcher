package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	envConfig "github.com/caarlos0/env/v11"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

// OIDCConfig holds the settings for the generic OIDC authentication provider.
// IssuerURL is the provider's issuer (e.g. "https://kc/realms/foo" for Keycloak
// or "https://authentik/application/o/argo-watcher/" for Authentik); the backend
// discovers the userinfo endpoint from it at runtime. The deprecated KEYCLOAK_*
// variables are mapped onto these fields by applyKeycloakCompat.
//
// TokenValidationInterval (milliseconds) bounds how stale a read authorization can be,
// capped per token by that token's own expiry. Zero revalidates every request, which
// multiplies UI refreshes into provider traffic.
//
// RequireTaskReadAuth closes GET /api/v1/tasks/{id}, the one read left open for
// clients that poll it without a credential. It is opt-in because turning it on
// fails every deployment driven by such a client; the unauthenticated_reads metric
// reports how many are left.
type OIDCConfig struct {
	Enabled                 bool     `env:"OIDC_ENABLED" json:"enabled"`
	IssuerURL               string   `env:"OIDC_ISSUER_URL" json:"issuer_url,omitempty"`
	ClientId                string   `env:"OIDC_CLIENT_ID" json:"client_id,omitempty"`
	TokenValidationInterval int      `env:"OIDC_TOKEN_VALIDATION_INTERVAL" envDefault:"300000" json:"token_validation_interval"`
	PrivilegedGroups        []string `env:"OIDC_PRIVILEGED_GROUPS" json:"privileged_groups,omitempty"`
	RequireTaskReadAuth     bool     `env:"OIDC_REQUIRE_TASK_READ_AUTH" json:"-"`
}

type DatabaseConfig struct {
	SSLMode string `env:"DB_SSL_MODE" envDefault:"disable"`
	// ConnectTimeout bounds the initial connection attempt (in seconds) so an
	// unreachable Postgres fails fast instead of blocking on the OS TCP timeout.
	// It is honored by both the pgx driver (server path) and libpq (migrations).
	ConnectTimeout int    `env:"DB_CONNECT_TIMEOUT" envDefault:"10"`
	TimeZone       string `env:"DB_TIMEZONE" envDefault:"UTC"`
	DSN            string `env:"DB_DSN,expand" envDefault:"host=${DB_HOST} port=${DB_PORT} user=${DB_USER} password=${DB_PASSWORD} dbname=${DB_NAME} sslmode=${DB_SSL_MODE} TimeZone=${DB_TIMEZONE}"`
}

// WebhookConfig describes the generic notification receiver. Only Enabled is public:
// the rest says how to reach a third party, and the URL is itself the credential.
type WebhookConfig struct {
	Enabled              bool   `env:"WEBHOOK_ENABLED" envDefault:"false" json:"enabled"`
	Url                  string `env:"WEBHOOK_URL" json:"-"`
	ContentType          string `env:"WEBHOOK_CONTENT_TYPE" envDefault:"application/json" json:"-"`
	Format               string `env:"WEBHOOK_FORMAT" json:"-"`
	AuthorizationHeader  string `env:"WEBHOOK_AUTHORIZATION_HEADER_NAME" envDefault:"Authorization" json:"-"`
	Token                string `env:"WEBHOOK_AUTHORIZATION_HEADER_VALUE" envDefault:"" json:"-"`
	AllowedResponseCodes []int  `env:"WEBHOOK_ALLOWED_RESPONSE_CODES" envDefault:"200" json:"-"`
}

// MattermostConfig follows WebhookConfig: only Enabled is public.
type MattermostConfig struct {
	Enabled       bool   `env:"MATTERMOST_ENABLED" envDefault:"false" json:"enabled"`
	Url           string `env:"MATTERMOST_URL" json:"-"`   // base URL of the Mattermost instance, without /api/v4
	Token         string `env:"MATTERMOST_TOKEN" json:"-"` // bot access token
	ChannelId     string `env:"MATTERMOST_CHANNEL_ID" json:"-"`
	Format        string `env:"MATTERMOST_FORMAT" json:"-"`                            // Go template rendering models.Task into the post markdown message
	MentionAuthor bool   `env:"MATTERMOST_MENTION_AUTHOR" envDefault:"false" json:"-"` // prepend @<Author> to every post
}

// ServerConfig is the server's runtime configuration. Its json tags double as the wire
// format of GET /api/v1/config, which cannot be authenticated — the frontend reads the
// OIDC issuer and client id from it before it can hold a token. A field marked
// `json:"-"` is excluded because it must not be public, not merely because the UI has
// no use for it. Consumers other than the UI read this payload too
// (github.com/shini4i/argo-watcher-mcp allowlists it field by field), so removing a
// key is a breaking change for them.
type ServerConfig struct {
	ArgoUrl            url.URL          `env:"ARGO_URL,required,notEmpty" json:"argo_cd_url"`
	ArgoUrlAlias       string           `env:"ARGO_URL_ALIAS" json:"argo_cd_url_alias,omitempty"` // Used to generate App Url. Can be omitted if ArgoUrl is reachable from outside.
	ArgoToken          string           `env:"ARGO_TOKEN,required,notEmpty" json:"-"`
	ArgoApiTimeout     int64            `env:"ARGO_API_TIMEOUT" envDefault:"60" json:"argo_api_timeout"`
	AcceptSuspendedApp bool             `env:"ACCEPT_SUSPENDED_APP" envDefault:"false" json:"accept_suspended_app"` // If true, we will accept "Suspended" health status as valid
	DeploymentTimeout  uint             `env:"DEPLOYMENT_TIMEOUT" envDefault:"900" json:"deployment_timeout"`
	ArgoRefreshApp     bool             `env:"ARGO_REFRESH_APP" envDefault:"true" json:"argo_refresh_app"`
	RegistryProxyUrl   string           `env:"DOCKER_IMAGES_PROXY" json:"registry_proxy_url,omitempty"`
	StateType          string           `env:"STATE_TYPE,required" json:"state_type"`
	StaticFilePath     string           `env:"STATIC_FILES_PATH" envDefault:"static" json:"-"`
	SkipTlsVerify      bool             `env:"SKIP_TLS_VERIFY" envDefault:"false" json:"skip_tls_verify"`
	LogLevel           string           `env:"LOG_LEVEL" envDefault:"info" json:"log_level"`
	Host               string           `env:"HOST" envDefault:"0.0.0.0" json:"-"`
	Port               string           `env:"PORT" envDefault:"8080" json:"-"`
	DeployToken        string           `env:"ARGO_WATCHER_DEPLOY_TOKEN" json:"-"`
	JWTSecret          string           `env:"JWT_SECRET" json:"-"`
	Db                 DatabaseConfig   `json:"-"`
	OIDC               OIDCConfig       `json:"oidc,omitempty"`
	LockdownSchedule   string           `env:"LOCKDOWN_SCHEDULE" json:"lockdown_schedule,omitempty"`
	Webhook            WebhookConfig    `json:"webhook,omitempty"`
	Mattermost         MattermostConfig `json:"mattermost,omitempty"`
	DevEnvironment     bool             `env:"DEV_ENVIRONMENT" envDefault:"false" json:"devEnvironment"` // Whether a set of dev specific setting should be turned on, do not touch unless you know what you are doing
	ArgoApiRetries     uint             `env:"ARGO_API_RETRIES" envDefault:"3" json:"argo_api_retries"`  // Total attempts (including initial); passed to retry.Attempts()
	RepoCachePath      string           `env:"REPO_CACHE_PATH" envDefault:"/data" json:"-"`
}

// MarshalJSON emits the OIDC block under both the canonical "oidc" key and a
// legacy "keycloak" key with identical content. The mirror preserves backward
// compatibility for older API consumers (and the e2e api-surface check) that
// still read config.keycloak.* after the rename from Keycloak-only auth. The
// `type alias` indirection drops ServerConfig's own MarshalJSON to avoid
// infinite recursion while keeping every field's json tag intact.
func (config ServerConfig) MarshalJSON() ([]byte, error) {
	type alias ServerConfig
	return json.Marshal(struct {
		alias
		Keycloak OIDCConfig `json:"keycloak"`
	}{
		alias:    alias(config),
		Keycloak: config.OIDC,
	})
}

// applyKeycloakCompat maps the deprecated KEYCLOAK_* environment variables onto
// the generic OIDC config whenever their OIDC_* counterparts are unset, so
// existing Keycloak deployments keep working unchanged after the rename. A
// single deprecation warning is logged when any KEYCLOAK_* variable is present.
//
// The Keycloak issuer is synthesized as "<KEYCLOAK_URL>/realms/<KEYCLOAK_REALM>"
// — exactly the issuer a Keycloak realm advertises — so OIDC discovery against
// it resolves the same userinfo endpoint the Keycloak-specific code targeted
// before.
func applyKeycloakCompat(cfg *ServerConfig) error {
	if !anyKeycloakVarSet() {
		return nil
	}

	slog.Warn("KEYCLOAK_* environment variables are deprecated; use OIDC_* instead " +
		"(see docs/reference/server-env.md). They remain honored for backward compatibility.")

	if val, ok := legacyValue("OIDC_ENABLED", "KEYCLOAK_ENABLED"); ok {
		// A malformed legacy value must fail startup rather than silently leave
		// auth disabled — a typo must never quietly drop the protected routes.
		parsed, err := strconv.ParseBool(strings.TrimSpace(val))
		if err != nil {
			return fmt.Errorf("invalid KEYCLOAK_ENABLED value %q: must be a boolean", val)
		}
		cfg.OIDC.Enabled = parsed
	}

	if _, ok := os.LookupEnv("OIDC_ISSUER_URL"); !ok {
		if issuer := keycloakIssuer(); issuer != "" {
			cfg.OIDC.IssuerURL = issuer
		}
	}

	if val, ok := legacyValue("OIDC_CLIENT_ID", "KEYCLOAK_CLIENT_ID"); ok {
		cfg.OIDC.ClientId = val
	}

	if val, ok := legacyValue("OIDC_TOKEN_VALIDATION_INTERVAL", "KEYCLOAK_TOKEN_VALIDATION_INTERVAL"); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return fmt.Errorf("invalid KEYCLOAK_TOKEN_VALIDATION_INTERVAL value %q: must be an integer", val)
		}
		cfg.OIDC.TokenValidationInterval = parsed
	}

	if val, ok := legacyValue("OIDC_PRIVILEGED_GROUPS", "KEYCLOAK_PRIVILEGED_GROUPS"); ok {
		cfg.OIDC.PrivilegedGroups = splitGroups(val)
	}

	return nil
}

// anyKeycloakVarSet reports whether any deprecated KEYCLOAK_* variable is present.
func anyKeycloakVarSet() bool {
	for _, v := range []string{
		"KEYCLOAK_ENABLED", "KEYCLOAK_URL", "KEYCLOAK_REALM", "KEYCLOAK_CLIENT_ID",
		"KEYCLOAK_TOKEN_VALIDATION_INTERVAL", "KEYCLOAK_PRIVILEGED_GROUPS",
	} {
		if _, ok := os.LookupEnv(v); ok {
			return true
		}
	}
	return false
}

// legacyValue returns the deprecated legacyKey's value, but only when the
// canonical primaryKey is unset (the OIDC_* variable always wins). It returns
// ("", false) when the primary is set or the legacy variable is absent.
func legacyValue(primaryKey, legacyKey string) (string, bool) {
	if _, ok := os.LookupEnv(primaryKey); ok {
		return "", false
	}
	return os.LookupEnv(legacyKey)
}

// keycloakIssuer synthesizes the OIDC issuer a Keycloak realm advertises from the
// deprecated KEYCLOAK_URL + KEYCLOAK_REALM pair, or "" if either is missing.
func keycloakIssuer() string {
	url := strings.TrimSpace(os.Getenv("KEYCLOAK_URL"))
	realm := strings.TrimSpace(os.Getenv("KEYCLOAK_REALM"))
	if url == "" || realm == "" {
		return ""
	}
	return strings.TrimRight(url, "/") + "/realms/" + realm
}

// splitGroups parses a comma-separated privileged-groups list, trimming blanks.
func splitGroups(val string) []string {
	var groups []string
	for _, g := range strings.Split(val, ",") {
		if trimmed := strings.TrimSpace(g); trimmed != "" {
			groups = append(groups, trimmed)
		}
	}
	return groups
}

// NewServerConfig parses the server configuration from environment variables.
// It validates that StateType is one of the allowed values. When parsing or
// validation fails the returned error names every offending field with a
// short description of the problem, so operators can fix the deployment in
// one pass.
func NewServerConfig() (*ServerConfig, error) {
	config, err := envConfig.ParseAs[ServerConfig]()
	if err != nil {
		return nil, helpers.PrettifyEnvError(err, "invalid argo-watcher server configuration:")
	}

	// Trim whitespace from tokens to prevent issues with trailing newlines from env vars
	config.ArgoToken = strings.TrimSpace(config.ArgoToken)
	config.DeployToken = strings.TrimSpace(config.DeployToken)
	config.JWTSecret = strings.TrimSpace(config.JWTSecret)
	config.Mattermost.Token = strings.TrimSpace(config.Mattermost.Token)

	// Map deprecated KEYCLOAK_* vars onto the generic OIDC config before
	// validation, so a synthesized Keycloak issuer is validated like any other.
	// A malformed legacy value fails startup rather than silently disabling auth.
	if err := applyKeycloakCompat(&config); err != nil {
		return nil, fmt.Errorf("invalid argo-watcher server configuration: %w", err)
	}

	if err := validateServerConfig(&config); err != nil {
		return nil, err
	}

	// Enforce the connect timeout even when DB_DSN is supplied explicitly (which
	// bypasses the default template), so an unreachable Postgres always fails fast
	// instead of blocking on the OS TCP timeout.
	if config.StateType == "postgres" {
		config.Db.DSN = ensureConnectTimeout(config.Db.DSN, config.Db.ConnectTimeout)
	}

	return &config, nil
}

// ensureConnectTimeout appends a connect_timeout parameter (in seconds) to a
// PostgreSQL DSN when it does not already specify one. An operator-provided
// connect_timeout is left untouched. Both the URI form (postgres://...) and the
// keyword/value form (host=... port=...) are supported.
func ensureConnectTimeout(dsn string, timeout int) string {
	if strings.Contains(dsn, "connect_timeout=") {
		return dsn
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		return fmt.Sprintf("%s%sconnect_timeout=%d", dsn, separator, timeout)
	}
	return fmt.Sprintf("%s connect_timeout=%d", dsn, timeout)
}

// validateServerConfig checks the semantic rules that env parsing cannot
// express (allowed enum values, numeric ranges). It reports every violation in
// one grouped message — mirroring helpers.PrettifyEnvError — so an operator can
// fix all of them in a single pass. Required-ness and non-emptiness are handled
// by the env `,required,notEmpty` tags during parsing, not here.
func validateServerConfig(config *ServerConfig) error {
	var problems []string
	if config.StateType != "postgres" && config.StateType != "in-memory" {
		problems = append(problems, fmt.Sprintf("  - StateType: must be one of [postgres in-memory], got %q", config.StateType))
	}
	if config.ArgoApiRetries < 1 || config.ArgoApiRetries > 10 {
		problems = append(problems, fmt.Sprintf("  - ArgoApiRetries: must be between 1 and 10, got %d", config.ArgoApiRetries))
	}
	// A non-positive connect timeout means "wait indefinitely" for both pgx and
	// libpq, silently defeating the fail-fast guard; only relevant for postgres.
	if config.StateType == "postgres" && config.Db.ConnectTimeout < 1 {
		problems = append(problems, fmt.Sprintf("  - ConnectTimeout: must be at least 1 second, got %d", config.Db.ConnectTimeout))
	}
	// When OIDC auth is enabled the issuer and client id are mandatory; discovery
	// and the login redirect cannot proceed without them.
	if config.OIDC.Enabled {
		if strings.TrimSpace(config.OIDC.IssuerURL) == "" {
			problems = append(problems, "  - OIDC.IssuerURL: must be set when OIDC auth is enabled (OIDC_ISSUER_URL, or legacy KEYCLOAK_URL + KEYCLOAK_REALM)")
		}
		if strings.TrimSpace(config.OIDC.ClientId) == "" {
			problems = append(problems, "  - OIDC.ClientId: must be set when OIDC auth is enabled (OIDC_CLIENT_ID, or legacy KEYCLOAK_CLIENT_ID)")
		}
	}
	// Rejected rather than ignored: with OIDC disabled no read is protected, so
	// honouring the switch alone would leave the endpoint open while the configuration
	// reads as though it were closed.
	if config.OIDC.RequireTaskReadAuth && !config.OIDC.Enabled {
		problems = append(problems, "  - OIDC.RequireTaskReadAuth: OIDC_REQUIRE_TASK_READ_AUTH requires OIDC_ENABLED=true; with OIDC disabled no read endpoint is protected")
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New("invalid argo-watcher server configuration:\ninvalid values:\n" +
		strings.Join(problems, "\n"))
}

// GetRetryAttempts returns the number of 15-second poll attempts that fit in
// DeploymentTimeout, plus one for the initial attempt.
func (config *ServerConfig) GetRetryAttempts() uint {
	return config.DeploymentTimeout/15 + 1
}
