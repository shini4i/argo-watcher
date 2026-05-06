package client

import (
	"errors"
	"sort"
	"strings"
	"time"

	envConfig "github.com/caarlos0/env/v11"
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

// NewClientConfig parses environment variables into a Config and returns the
// new instance or an error. When parsing fails the returned error groups
// missing required variables and invalid values under separate headers, so
// the user can fix everything in one pass.
func NewClientConfig() (*Config, error) {
	config, err := envConfig.ParseAs[Config]()
	if err != nil {
		return nil, prettifyEnvError(err, "invalid argo-watcher client configuration:")
	}
	return &config, nil
}

// prettifyEnvError reformats github.com/caarlos0/env's semicolon-joined
// AggregateError into a readable multi-line error. VarIsNotSetError entries
// are listed under "missing required environment variables"; everything else
// (parse errors, empty-var errors, etc.) goes under "invalid values".
// Returns the original error unchanged when it is not an AggregateError or
// when no errors are extracted.
func prettifyEnvError(err error, leadIn string) error {
	var agg envConfig.AggregateError
	if !errors.As(err, &agg) {
		return err
	}

	var missing, invalid []string
	for _, e := range agg.Errors {
		var notSet envConfig.VarIsNotSetError
		if errors.As(e, &notSet) {
			missing = append(missing, "  - "+notSet.Key)
			continue
		}
		invalid = append(invalid, "  - "+e.Error())
	}
	sort.Strings(missing)
	sort.Strings(invalid)

	var sb strings.Builder
	sb.WriteString(leadIn)
	if len(missing) > 0 {
		sb.WriteString("\nmissing required environment variables:\n")
		sb.WriteString(strings.Join(missing, "\n"))
	}
	if len(invalid) > 0 {
		sb.WriteString("\ninvalid values:\n")
		sb.WriteString(strings.Join(invalid, "\n"))
	}
	return errors.New(sb.String())
}
