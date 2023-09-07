package config

import (
	"errors"
	"strconv"

	"github.com/shini4i/argo-watcher/internal/helpers"

	envConfig "github.com/caarlos0/env/v9"
)

const (
	LogFormatText = "text"
)

type ServerConfig struct {
	ArgoUrl          string `env:"ARGO_URL,required"`
	ArgoToken        string `env:"ARGO_TOKEN,required"`
	ArgoApiTimeout   string `env:"ARGO_API_TIMEOUT" envDefault:"60"`
	ArgoTimeout      string `env:"ARGO_TIMEOUT" envDefault:"0"`
	ArgoRefreshApp   bool   `env:"ARGO_REFRESH_APP" envDefault:"true"`
	RegistryProxyUrl string `env:"DOCKER_IMAGES_PROXY"`
	StateType        string `env:"STATE_TYPE"`
	StaticFilePath   string `env:"STATIC_FILES_PATH" envDefault:"static"`
	SkipTlsVerify    string `env:"SKIP_TLS_VERIFY" envDefault:"false"`
	LogLevel         string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat        string `env:"LOG_FORMAT" envDefault:"json"`
	Host             string `env:"HOST" envDefault:"0.0.0.0"`
	Port             string `env:"PORT" envDefault:"8080"`
	DbHost           string `env:"DB_HOST" envDefault:"localhost"`
	DbPort           string `env:"DB_PORT" envDefault:"5432"`
	DbName           string `env:"DB_NAME"`
	DbUser           string `env:"DB_USER"`
	DbPassword       string `env:"DB_PASSWORD"`
	DbMigrationsPath string `env:"DB_MIGRATIONS_PATH" envDefault:"db/migrations"` // deprecated
	DeployToken      string `env:"ARGO_WATCHER_DEPLOY_TOKEN"`
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

// GetRetryAttempts calculates the number of retry attempts based on the Argo timeout value in the server configuration.
// It converts the Argo timeout to an integer value and divides it by 15 to determine the number of 15-second intervals.
// The calculated value is incremented by 1 to account for the initial attempt.
// It returns the number of retry attempts as an unsigned integer.
func (config *ServerConfig) GetRetryAttempts() uint {
	argoTimeout, _ := strconv.Atoi(config.ArgoTimeout)
	return uint((argoTimeout / 15) + 1)
}
