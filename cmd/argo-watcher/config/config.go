package config

import (
	"errors"
	"net/url"

	"github.com/shini4i/argo-watcher/internal/helpers"

	envConfig "github.com/caarlos0/env/v9"
)

const (
	LogFormatText = "text"
)

type ServerConfig struct {
	ArgoUrl url.URL `env:"ARGO_URL,required" json:"argo_cd_url"`
	// ArgoUrlAlias is used to replace the ArgoUrl in the UI. This is useful when the ArgoUrl is an internal URL
	ArgoUrlAlias      string `env:"ARGO_URL_ALIAS" json:"argo_cd_url_alias,omitempty"`
	ArgoToken         string `env:"ARGO_TOKEN,required" json:"-"`
	ArgoApiTimeout    int64  `env:"ARGO_API_TIMEOUT" envDefault:"60" json:"argo_api_timeout"`
	DeploymentTimeout uint   `env:"DEPLOYMENT_TIMEOUT" envDefault:"300" json:"deployment_timeout"`
	ArgoRefreshApp    bool   `env:"ARGO_REFRESH_APP" envDefault:"true" json:"argo_refresh_app"`
	RegistryProxyUrl  string `env:"DOCKER_IMAGES_PROXY" json:"registry_proxy_url,omitempty"`
	StateType         string `env:"STATE_TYPE,required" json:"state_type"`
	StaticFilePath    string `env:"STATIC_FILES_PATH" envDefault:"static" json:"-"`
	SkipTlsVerify     bool   `env:"SKIP_TLS_VERIFY" envDefault:"false" json:"skip_tls_verify"`
	LogLevel          string `env:"LOG_LEVEL" envDefault:"info" json:"log_level"`
	LogFormat         string `env:"LOG_FORMAT" envDefault:"json" json:"-"`
	Host              string `env:"HOST" envDefault:"0.0.0.0" json:"-"`
	Port              string `env:"PORT" envDefault:"8080" json:"-"`
	DbHost            string `env:"DB_HOST" json:"db_host,omitempty"`
	DbPort            int    `env:"DB_PORT" json:"db_port,omitempty"`
	DbName            string `env:"DB_NAME" json:"db_name,omitempty"`
	DbUser            string `env:"DB_USER" json:"db_user,omitempty"`
	DbPassword        string `env:"DB_PASSWORD" json:"-"`
	DbMigrationsPath  string `env:"DB_MIGRATIONS_PATH" envDefault:"db/migrations" json:"-"`
	DeployToken       string `env:"ARGO_WATCHER_DEPLOY_TOKEN" json:"-"`
}

// NewServerConfig parses the server configuration from environment variables using the envconfig package.
// It performs custom checks to ensure that the StateType is a valid value.
// If the StateType is empty or not one of the allowed types ("postgres" or "in-memory"), it returns an error.
// Otherwise, it returns the parsed server configuration and any error encountered during the parsing process.
func NewServerConfig() (*ServerConfig, error) {
	// parse config
	var (
		err    error
		config ServerConfig
	)

	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}

	// custom checks
	allowedTypes := []string{"postgres", "in-memory"}
	if config.StateType == "" || !helpers.Contains(allowedTypes, config.StateType) {
		return nil, errors.New("variable STATE_TYPE must be one of [\"postgres\", \"in-memory\"]")
	}

	// return config
	return &config, err
}

// GetRetryAttempts calculates the number of retry attempts based on the Deployment timeout value in the server configuration.
// It divides it by 15 to determine the number of 15-second intervals.
// The calculated value is incremented by 1 to account for the initial attempt.
// It returns the number of retry attempts as an unsigned integer.
func (config *ServerConfig) GetRetryAttempts() uint {
	return (config.DeploymentTimeout / 15) + 1
}
