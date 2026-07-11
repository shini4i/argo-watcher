package client

import (
	"time"

	envConfig "github.com/caarlos0/env/v11"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

type Config struct {
	Url          string        `env:"ARGO_WATCHER_URL,required"`
	Images       []string      `env:"IMAGES,required"`
	Tag          string        `env:"IMAGE_TAG,required"`
	App          string        `env:"ARGO_APP,required"`
	Author       string        `env:"COMMIT_AUTHOR,required"`
	Project      string        `env:"PROJECT_NAME,required"`
	Token        string        `env:"ARGO_WATCHER_DEPLOY_TOKEN"`
	JsonWebToken string        `env:"BEARER_TOKEN"`
	Timeout      time.Duration `env:"TIMEOUT" envDefault:"60s"`
	TaskTimeout  int           `env:"TASK_TIMEOUT"`
	// Refresh optionally overrides the server's instance-wide refresh setting for this deployment.
	// Left unset it stays nil and the field is omitted from the request, so the server keeps its
	// default; set TASK_REFRESH=true/false to force a refresh on or off for this task (issue #334).
	Refresh                *bool         `env:"TASK_REFRESH"`
	RetryInterval          time.Duration `env:"RETRY_INTERVAL" envDefault:"15s"`
	ExpectedDeploymentTime time.Duration `env:"EXPECTED_DEPLOY_TIME" envDefault:"15m"`
	Debug                  bool          `env:"DEBUG"`
}

// NewClientConfig parses environment variables into a Config and returns the
// new instance or an error. When parsing fails the returned error groups
// missing required variables and invalid values under separate headers, so
// the user can fix everything in one pass.
func NewClientConfig() (*Config, error) {
	config, err := envConfig.ParseAs[Config]()
	if err != nil {
		return nil, helpers.PrettifyEnvError(err, "invalid argo-watcher client configuration:")
	}
	return &config, nil
}
