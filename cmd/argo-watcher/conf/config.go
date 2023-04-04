package conf

import (
	"errors"

	envConfig "github.com/kelseyhightower/envconfig"
	"github.com/shini4i/argo-watcher/internal/helpers"
)

type Container struct {
	ArgoUrl string `required:"false" envconfig:"ARGO_URL"` 
	ArgoToken string `required:"false" envconfig:"ARGO_TOKEN"` 
    ArgoApiTimeout string `required:"false" envconfig:"ARGO_API_TIMEOUT" default:"60"` 
	ArgoTimeout string `required:"false" envconfig:"ARGO_TIMEOUT" default:"0"` 
	StateType string `required:"false" envconfig:"STATE_TYPE"` 
	StaticFilePath string `required:"false" envconfig:"STATIC_FILES_PATH" default:"static"` 
	LogLevel string `required:"false" envconfig:"LOG_LEVEL" default:"info"` 
	Host string `required:"false" envconfig:"HOST" default:"0.0.0.0"` 
	Port string `required:"false" envconfig:"PORT" default:"8080"` 
	DbHost string `required:"false" envconfig:"DB_HOST" default:"localhost"` 
	DbPort string `required:"false" envconfig:"DB_PORT" default:"5432"` 
	DbName string `required:"false" envconfig:"DB_NAME"` 
	DbUser string `required:"false" envconfig:"DB_USER"` 
	DbPassword string `required:"false" envconfig:"DB_PASSWORD"` 
	DbMigrationsPath string `required:"false" envconfig:"DB_MIGRATIONS_PATH" default:"db/migrations"` 
	SkipTlsVerify string `required:"false" envconfig:"SKIP_TLS_VERIFY" default:"false"` 
}

func InitConfig() (*Container, error) {
	// parse config
	var config Container
    err := envConfig.Process("", &config)
	// custom checks
	allowedTypes := []string {"postgres", "in-memory"}
	if config.StateType == "" || !helpers.Contains(allowedTypes, config.StateType) {
		return nil, errors.New("variable STATE_TYPE must be one of [\"postgres\", \"in-memory\"]")
	}
	// return config
    return &config, err
}
