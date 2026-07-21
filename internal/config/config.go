package config

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	envConfig "github.com/caarlos0/env/v11"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

type KeycloakConfig struct {
	Enabled                 bool     `env:"KEYCLOAK_ENABLED" json:"enabled"`
	Url                     string   `env:"KEYCLOAK_URL" json:"url,omitempty"`
	Realm                   string   `env:"KEYCLOAK_REALM" json:"realm,omitempty"`
	ClientId                string   `env:"KEYCLOAK_CLIENT_ID" json:"client_id,omitempty"`
	TokenValidationInterval int      `env:"KEYCLOAK_TOKEN_VALIDATION_INTERVAL" envDefault:"10000" json:"token_validation_interval"`
	PrivilegedGroups        []string `env:"KEYCLOAK_PRIVILEGED_GROUPS" json:"privileged_groups,omitempty"`
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

type WebhookConfig struct {
	Enabled              bool   `env:"WEBHOOK_ENABLED" envDefault:"false" json:"enabled"`
	Url                  string `env:"WEBHOOK_URL" json:"url,omitempty"`
	ContentType          string `env:"WEBHOOK_CONTENT_TYPE" envDefault:"application/json" json:"content_type,omitempty"`
	Format               string `env:"WEBHOOK_FORMAT" json:"format,omitempty"`
	AuthorizationHeader  string `env:"WEBHOOK_AUTHORIZATION_HEADER_NAME" envDefault:"Authorization" json:"authorization_header,omitempty"`
	Token                string `env:"WEBHOOK_AUTHORIZATION_HEADER_VALUE" envDefault:"" json:"-"`
	AllowedResponseCodes []int  `env:"WEBHOOK_ALLOWED_RESPONSE_CODES" envDefault:"200" json:"allowed_response_codes,omitempty"`
}

type MattermostConfig struct {
	Enabled       bool   `env:"MATTERMOST_ENABLED" envDefault:"false" json:"enabled"`
	Url           string `env:"MATTERMOST_URL" json:"url,omitempty"` // base URL of the Mattermost instance, without /api/v4
	Token         string `env:"MATTERMOST_TOKEN" json:"-"`           // bot access token
	ChannelId     string `env:"MATTERMOST_CHANNEL_ID" json:"channel_id,omitempty"`
	Format        string `env:"MATTERMOST_FORMAT" json:"format,omitempty"`                          // Go template rendering models.Task into the post markdown message
	MentionAuthor bool   `env:"MATTERMOST_MENTION_AUTHOR" envDefault:"false" json:"mention_author"` // prepend @<Author> to every post
}

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
	Keycloak           KeycloakConfig   `json:"keycloak,omitempty"`
	LockdownSchedule   string           `env:"LOCKDOWN_SCHEDULE" json:"lockdown_schedule,omitempty"`
	Webhook            WebhookConfig    `json:"webhook,omitempty"`
	Mattermost         MattermostConfig `json:"mattermost,omitempty"`
	DevEnvironment     bool             `env:"DEV_ENVIRONMENT" envDefault:"false" json:"devEnvironment"` // Whether a set of dev specific setting should be turned on, do not touch unless you know what you are doing
	ArgoApiRetries     uint             `env:"ARGO_API_RETRIES" envDefault:"3" json:"argo_api_retries"`  // Total attempts (including initial); passed to retry.Attempts()
	RepoCachePath      string           `env:"REPO_CACHE_PATH" envDefault:"/data" json:"-"`
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
