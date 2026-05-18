package config

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

// validate is package-scoped because validator.New caches struct reflection
// metadata; reusing one instance across calls avoids re-doing that work.
var validate = validator.New()

type KeycloakConfig struct {
	Enabled                 bool     `env:"KEYCLOAK_ENABLED" json:"enabled"`
	Url                     string   `env:"KEYCLOAK_URL" json:"url,omitempty"`
	Realm                   string   `env:"KEYCLOAK_REALM" json:"realm,omitempty"`
	ClientId                string   `env:"KEYCLOAK_CLIENT_ID" json:"client_id,omitempty"`
	TokenValidationInterval int      `env:"KEYCLOAK_TOKEN_VALIDATION_INTERVAL" envDefault:"10000" json:"token_validation_interval"`
	PrivilegedGroups        []string `env:"KEYCLOAK_PRIVILEGED_GROUPS" json:"privileged_groups,omitempty"`
}

type DatabaseConfig struct {
	SSLMode  string `env:"DB_SSL_MODE" envDefault:"disable"`
	TimeZone string `env:"DB_TIMEZONE" envDefault:"UTC"`
	DSN      string `env:"DB_DSN,expand" envDefault:"host=${DB_HOST} port=${DB_PORT} user=${DB_USER} password=${DB_PASSWORD} dbname=${DB_NAME} sslmode=${DB_SSL_MODE} TimeZone=${DB_TIMEZONE}"`
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

type ServerConfig struct {
	ArgoUrl            url.URL        `env:"ARGO_URL,required" json:"argo_cd_url"`
	ArgoUrlAlias       string         `env:"ARGO_URL_ALIAS" json:"argo_cd_url_alias,omitempty"` // Used to generate App Url. Can be omitted if ArgoUrl is reachable from outside.
	ArgoToken          string         `env:"ARGO_TOKEN,required" json:"-"`
	ArgoApiTimeout     int64          `env:"ARGO_API_TIMEOUT" envDefault:"60" json:"argo_api_timeout"`
	AcceptSuspendedApp bool           `env:"ACCEPT_SUSPENDED_APP" envDefault:"false" json:"accept_suspended_app"` // If true, we will accept "Suspended" health status as valid
	DeploymentTimeout  uint           `env:"DEPLOYMENT_TIMEOUT" envDefault:"900" json:"deployment_timeout"`
	ArgoRefreshApp     bool           `env:"ARGO_REFRESH_APP" envDefault:"true" json:"argo_refresh_app"`
	RegistryProxyUrl   string         `env:"DOCKER_IMAGES_PROXY" json:"registry_proxy_url,omitempty"`
	StateType          string         `env:"STATE_TYPE,required" validate:"oneof=postgres in-memory" json:"state_type"`
	StaticFilePath     string         `env:"STATIC_FILES_PATH" envDefault:"static" json:"-"`
	SkipTlsVerify      bool           `env:"SKIP_TLS_VERIFY" envDefault:"false" json:"skip_tls_verify"`
	LogLevel           string         `env:"LOG_LEVEL" envDefault:"info" json:"log_level"`
	Host               string         `env:"HOST" envDefault:"0.0.0.0" json:"-"`
	Port               string         `env:"PORT" envDefault:"8080" json:"-"`
	DeployToken        string         `env:"ARGO_WATCHER_DEPLOY_TOKEN" json:"-"`
	JWTSecret          string         `env:"JWT_SECRET" json:"-"`
	Db                 DatabaseConfig `json:"-"`
	Keycloak           KeycloakConfig `json:"keycloak,omitempty"`
	LockdownSchedule   string         `env:"LOCKDOWN_SCHEDULE" json:"lockdown_schedule,omitempty"`
	Webhook            WebhookConfig  `json:"webhook,omitempty"`
	DevEnvironment     bool           `env:"DEV_ENVIRONMENT" envDefault:"false" json:"devEnvironment"` // Whether a set of dev specific setting should be turned on, do not touch unless you know what you are doing
	ArgoApiRetries     uint           `env:"ARGO_API_RETRIES" envDefault:"3" validate:"min=1,max=10" json:"argo_api_retries"` // Total attempts (including initial); passed to retry.Attempts()
	RepoCachePath      string         `env:"REPO_CACHE_PATH" envDefault:"/data" json:"-"`
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

	if err := validate.Struct(&config); err != nil {
		return nil, prettifyValidatorError(err)
	}

	return &config, nil
}

// prettifyValidatorError reformats go-playground/validator's
// `Key: 'ServerConfig.X' Error:Field validation for 'X' failed on the 'tag'`
// blob into one-line-per-field readable output. The Go field name (e.g.
// StateType) maps obviously to its env var (STATE_TYPE) for any operator;
// no translation is attempted.
func prettifyValidatorError(err error) error {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return err
	}

	lines := make([]string, 0, len(ve))
	for _, fe := range ve {
		lines = append(lines, fmt.Sprintf("  - %s: %s", fe.Field(), describeValidatorFailure(fe)))
	}
	sort.Strings(lines)
	return errors.New("invalid argo-watcher server configuration:\ninvalid values:\n" +
		strings.Join(lines, "\n"))
}

// describeValidatorFailure renders a single validator error as a short human
// phrase. Falls back to "<tag> validation failed" for tags this function
// does not specialise.
func describeValidatorFailure(fe validator.FieldError) string {
	switch fe.Tag() {
	case "oneof":
		return fmt.Sprintf("must be one of [%s], got %q", fe.Param(), fe.Value())
	case "min":
		return fmt.Sprintf("must be >= %s, got %v", fe.Param(), fe.Value())
	case "max":
		return fmt.Sprintf("must be <= %s, got %v", fe.Param(), fe.Value())
	default:
		return fmt.Sprintf("%s validation failed (got %v)", fe.Tag(), fe.Value())
	}
}

// GetRetryAttempts calculates the number of retry attempts based on the Deployment timeout value in the server configuration.
// It divides it by 15 to determine the number of 15-second intervals.
// The calculated value is incremented by 1 to account for the initial attempt.
// It returns the number of retry attempts as an unsigned integer.
func (config *ServerConfig) GetRetryAttempts() uint {
	return config.DeploymentTimeout/15 + 1
}
