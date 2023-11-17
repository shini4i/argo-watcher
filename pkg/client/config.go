package client

import (
	"time"

	envConfig "github.com/caarlos0/env/v9"
)

type ClientConfig struct {
	Url     string        `env:"ARGO_WATCHER_URL"`
	Images  []string      `env:"IMAGES"`
	Tag     string        `env:"IMAGE_TAG"`
	App     string        `env:"ARGO_APP"`
	Author  string        `env:"COMMIT_AUTHOR"`
	Project string        `env:"PROJECT_NAME"`
	Token   string        `env:"ARGO_WATCHER_DEPLOY_TOKEN"`
	Timeout time.Duration `env:"TIMEOUT" envDefault:"60s"`
	Debug   bool          `env:"DEBUG"`
}

func NewClientConfig() (*ClientConfig, error) {
	var config ClientConfig

	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
