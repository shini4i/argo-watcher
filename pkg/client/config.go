package client

import (
	"time"

	envConfig "github.com/caarlos0/env/v10"
)

type Config struct {
	Url                    string        `env:"ARGO_WATCHER_URL,required"`
	Images                 []string      `env:"IMAGES,required"`
	Tag                    string        `env:"IMAGE_TAG,required"`
	App                    string        `env:"ARGO_APP,required"`
	Author                 string        `env:"COMMIT_AUTHOR,required"`
	Project                string        `env:"PROJECT_NAME,required"`
	Token                  string        `env:"ARGO_WATCHER_DEPLOY_TOKEN"`
	JsonWebToken           string        `env:"BEARER_TOKEN"`
	Timeout                time.Duration `env:"TIMEOUT" envDefault:"60s"`
	TaskTimeout            int           `env:"TASK_TIMEOUT"`
	RetryInterval          time.Duration `env:"RETRY_INTERVAL" envDefault:"15s"`
	ExpectedDeploymentTime time.Duration `env:"EXPECTED_DEPLOY_TIME" envDefault:"15m"`
	Debug                  bool          `env:"DEBUG"`
}

func NewClientConfig() (*Config, error) {
	var config Config

	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
