package config

import (
	"errors"
	"strconv"

	"github.com/shini4i/argo-watcher/internal/helpers"

	envConfig "github.com/kelseyhightower/envconfig"
)

const (
	LOG_FORMAT_TEXT = "text"
)

type ServerConfig struct {
	ArgoUrl          string `required:"true" envconfig:"ARGO_URL"`
	ArgoToken        string `required:"true" envconfig:"ARGO_TOKEN"`
	ArgoApiTimeout   string `required:"false" envconfig:"ARGO_API_TIMEOUT" default:"60"`
	ArgoTimeout      string `required:"false" envconfig:"ARGO_TIMEOUT" default:"0"`
	ArgoRefreshApp   bool   `required:"false" envconfig:"ARGO_REFRESH_APP" default:"true"`
	RegistryProxyUrl string `required:"false" envconfig:"DOCKER_IMAGES_PROXY"`
	StateType        string `required:"false" envconfig:"STATE_TYPE"`
	StaticFilePath   string `required:"false" envconfig:"STATIC_FILES_PATH" default:"static"`
	SkipTlsVerify    string `required:"false" envconfig:"SKIP_TLS_VERIFY" default:"false"`
	LogLevel         string `required:"false" envconfig:"LOG_LEVEL" default:"info"`
	LogFormat        string `required:"false" envconfig:"LOG_FORMAT" default:"json"`
	Host             string `required:"false" envconfig:"HOST" default:"0.0.0.0"`
	Port             string `required:"false" envconfig:"PORT" default:"8080"`
	DbHost           string `required:"false" envconfig:"DB_HOST" default:"localhost"`
	DbPort           string `required:"false" envconfig:"DB_PORT" default:"5432"`
	DbName           string `required:"false" envconfig:"DB_NAME"`
	DbUser           string `required:"false" envconfig:"DB_USER"`
	DbPassword       string `required:"false" envconfig:"DB_PASSWORD"`
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

	if err := envConfig.Process("", &config); err != nil {
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
