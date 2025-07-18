package config

import (
	"net/url"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
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
}

// NewServerConfig parses the server configuration from environment variables using the envconfig package.
// It performs custom checks to ensure that the StateType is a valid value.
// If the StateType is empty or not one of the allowed types ("postgres" or "in-memory"), it returns an error.
// Otherwise, it returns the parsed server configuration and any error encountered during the parsing process.
func NewServerConfig() (*ServerConfig, error) {
	var err error
	var config ServerConfig

	if config, err = envConfig.ParseAs[ServerConfig](); err != nil {
		return nil, err
	}

	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, err
	}

	// return config
	return &config, err
}

// GetRetryAttempts calculates the number of retry attempts based on the Deployment timeout value in the server configuration.
// It divides it by 15 to determine the number of 15-second intervals.
// The calculated value is incremented by 1 to account for the initial attempt.
// It returns the number of retry attempts as an unsigned integer.
func (config *ServerConfig) GetRetryAttempts() uint {
	return config.DeploymentTimeout/15 + 1
}
