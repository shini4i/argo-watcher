package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"

	envConfig "github.com/caarlos0/env/v11"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

// maxTaskRetentionDays is the longest accepted retention window, a century. It
// exists to reject a mistyped value at startup: a window long enough to push the
// cutoff outside the timestamp range Postgres can represent fails inside the
// sweep instead, once an hour, without naming the setting at fault.
const maxTaskRetentionDays = 36500

// URL is a url.URL that crosses every boundary as a URL string: the JSON of
// GET /api/v1/config and the structured logs, which would otherwise carry the
// eleven exported fields of a url.URL for every consumer to reassemble. It parses
// from a string too, so both env parsing and JSON decoding accept the same form.
type URL struct {
	url.URL
}

// MarshalText renders the URL as a string, minus any userinfo. encoding/json uses
// it, so the value serialises as "https://argo-cd.example.com" rather than as an
// object. Userinfo is dropped because GET /api/v1/config is unauthenticated and
// url.URL.String() renders basic-auth credentials, password included.
func (u URL) MarshalText() ([]byte, error) {
	u.User = nil
	return []byte(u.String()), nil
}

// UnmarshalText parses a URL string, rejecting one url.Parse cannot read. It backs
// both env parsing and JSON decoding of the config payload.
func (u *URL) UnmarshalText(text []byte) error {
	parsed, err := url.Parse(string(text))
	if err != nil {
		return err
	}
	u.URL = *parsed
	return nil
}

// OIDCConfig holds the settings for the generic OIDC authentication provider.
// IssuerURL is the provider's issuer (e.g. "https://kc/realms/foo" for Keycloak
// or "https://authentik/application/o/argo-watcher/" for Authentik); the backend
// discovers the userinfo endpoint from it at runtime.
//
// TokenValidationInterval (milliseconds) bounds how stale a read authorization can be,
// capped per token by that token's own expiry. Zero revalidates every request, which
// multiplies UI refreshes into provider traffic.
//
// RequireTaskReadAuth closes GET /api/v1/tasks/{id}, the one read left open for
// clients that poll it without a credential. It is opt-in because turning it on
// fails every deployment driven by such a client; the unauthenticated_reads metric
// reports how many are left.
//
// GravatarFallback is opt-in because it discloses the user's email address to
// gravatar.com — hashed, but reversible for any address worth guessing.
type OIDCConfig struct {
	Enabled                 bool     `env:"OIDC_ENABLED" json:"enabled"`
	IssuerURL               string   `env:"OIDC_ISSUER_URL" json:"issuer_url,omitempty"`
	ClientId                string   `env:"OIDC_CLIENT_ID" json:"client_id,omitempty"`
	TokenValidationInterval int      `env:"OIDC_TOKEN_VALIDATION_INTERVAL" envDefault:"300000" json:"token_validation_interval"`
	PrivilegedGroups        []string `env:"OIDC_PRIVILEGED_GROUPS" json:"privileged_groups,omitempty"`
	RequireTaskReadAuth     bool     `env:"OIDC_REQUIRE_TASK_READ_AUTH" json:"-"`
	GravatarFallback        bool     `env:"OIDC_GRAVATAR_FALLBACK" json:"gravatar_fallback"`
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
	ArgoUrl            URL              `env:"ARGO_URL,required,notEmpty" json:"argo_cd_url" swaggertype:"primitive,string"`
	ArgoUrlAlias       string           `env:"ARGO_URL_ALIAS" json:"argo_cd_url_alias,omitempty"` // Used to generate App Url. Can be omitted if ArgoUrl is reachable from outside.
	ArgoToken          string           `env:"ARGO_TOKEN,required,notEmpty" json:"-"`
	ArgoApiTimeout     int64            `env:"ARGO_API_TIMEOUT" envDefault:"60" json:"argo_api_timeout"`
	AcceptSuspendedApp bool             `env:"ACCEPT_SUSPENDED_APP" envDefault:"false" json:"accept_suspended_app"`
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
	// TaskRetentionEnabled turns on the periodic removal of finished tasks older
	// than TaskRetentionDays, and TaskRetentionDays is that window in days. Both
	// only apply to the postgres state; in-memory tasks live no longer than the
	// process. Deleting deployment history is irreversible, so it is opt-in.
	TaskRetentionEnabled bool `env:"TASK_RETENTION_ENABLED" envDefault:"false" json:"-"`
	TaskRetentionDays    int  `env:"TASK_RETENTION_DAYS" envDefault:"365" json:"-"`
}

// removedKeycloakVars pairs each KEYCLOAK_* variable removed in 1.0.0 with the
// OIDC_* setting that replaces it.
var removedKeycloakVars = []struct{ legacy, replacement string }{
	{"KEYCLOAK_ENABLED", "OIDC_ENABLED"},
	{"KEYCLOAK_URL", "OIDC_ISSUER_URL (as <KEYCLOAK_URL>/realms/<KEYCLOAK_REALM>)"},
	{"KEYCLOAK_REALM", "OIDC_ISSUER_URL (as <KEYCLOAK_URL>/realms/<KEYCLOAK_REALM>)"},
	{"KEYCLOAK_CLIENT_ID", "OIDC_CLIENT_ID"},
	{"KEYCLOAK_TOKEN_VALIDATION_INTERVAL", "OIDC_TOKEN_VALIDATION_INTERVAL"},
	{"KEYCLOAK_PRIVILEGED_GROUPS", "OIDC_PRIVILEGED_GROUPS"},
}

// trimEntries strips surrounding whitespace from each entry and drops the blanks a
// trailing or doubled separator leaves behind.
func trimEntries(values []string) []string {
	var trimmed []string
	for _, v := range values {
		if entry := strings.TrimSpace(v); entry != "" {
			trimmed = append(trimmed, entry)
		}
	}
	return trimmed
}

// rejectRemovedKeycloakVars fails startup when any KEYCLOAK_* variable is still set,
// naming each one and its OIDC_* replacement. Ignoring them would be fail-open: a
// deployment carrying only KEYCLOAK_ENABLED=true would boot with authentication off,
// serving every read to anyone, with nothing in the log naming the cause.
func rejectRemovedKeycloakVars() error {
	var problems []string
	for _, v := range removedKeycloakVars {
		if _, ok := os.LookupEnv(v.legacy); ok {
			problems = append(problems, fmt.Sprintf("  - %s: removed in 1.0.0, use %s", v.legacy, v.replacement))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New("invalid argo-watcher server configuration:\nremoved variables:\n" +
		strings.Join(problems, "\n"))
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
	// Group membership is matched exactly against the provider's claim, so a spaced
	// list entry would silently deny every privileged action to that group.
	config.OIDC.PrivilegedGroups = trimEntries(config.OIDC.PrivilegedGroups)

	if err := rejectRemovedKeycloakVars(); err != nil {
		return nil, err
	}

	if err := validateServerConfig(&config); err != nil {
		return nil, err
	}

	// Retention prunes rows from the tasks table, which the in-memory state does
	// not have. Warning beats failing: the setting is inert, not contradictory.
	if config.TaskRetentionEnabled && config.StateType != "postgres" {
		slog.Warn("TASK_RETENTION_ENABLED has no effect with STATE_TYPE=in-memory; task history is only persisted by the postgres state.")
	}

	// Enforce the connect timeout even when DB_DSN is supplied explicitly (which
	// bypasses the default template), so an unreachable Postgres always fails fast
	// instead of blocking on the OS TCP timeout.
	if config.StateType == "postgres" {
		config.Db.DSN = ensureConnectTimeout(config.Db.DSN, config.Db.ConnectTimeout)
	}

	return &config, nil
}

// ensureConnectTimeout appends a connect_timeout parameter (in seconds) to a PostgreSQL
// DSN, in either the URI or keyword/value form, when it does not already specify one.
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

// taskRetentionProblems reports what makes the retention window unusable, or
// nothing when retention is off — with the toggle disabled the window is inert.
//
// A non-positive window puts the cutoff at or after now, so the first sweep
// would delete the entire deployment history. Beyond a century the cutoff falls
// outside the range Postgres can represent and every sweep fails, which would
// surface as an hourly error rather than as a bad setting.
func taskRetentionProblems(config *ServerConfig) []string {
	if !config.TaskRetentionEnabled {
		return nil
	}
	if config.TaskRetentionDays < 1 || config.TaskRetentionDays > maxTaskRetentionDays {
		return []string{fmt.Sprintf("  - TaskRetentionDays: must be between 1 and %d days, got %d", maxTaskRetentionDays, config.TaskRetentionDays)}
	}
	return nil
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
			problems = append(problems, "  - OIDC.IssuerURL: must be set when OIDC auth is enabled (OIDC_ISSUER_URL)")
		}
		if strings.TrimSpace(config.OIDC.ClientId) == "" {
			problems = append(problems, "  - OIDC.ClientId: must be set when OIDC auth is enabled (OIDC_CLIENT_ID)")
		}
	}
	// Rejected rather than ignored: with OIDC disabled no read is protected, so
	// honouring the switch alone would leave the endpoint open while the configuration
	// reads as though it were closed.
	if config.OIDC.RequireTaskReadAuth && !config.OIDC.Enabled {
		problems = append(problems, "  - OIDC.RequireTaskReadAuth: OIDC_REQUIRE_TASK_READ_AUTH requires OIDC_ENABLED=true; with OIDC disabled no read endpoint is protected")
	}
	problems = append(problems, taskRetentionProblems(config)...)

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
