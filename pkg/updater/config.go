package updater

import (
	"fmt"
	"time"

	envConfig "github.com/caarlos0/env/v11"
)

// GitConfig holds runtime configuration for the git updater, parsed from
// environment variables on startup.
type GitConfig struct {
	SshKeyPath          string        `env:"SSH_KEY_PATH,required"`
	SshKeyPass          string        `env:"SSH_KEY_PASS"`
	SshCommitUser       string        `env:"SSH_COMMIT_USER" envDefault:"argo-watcher"`
	SshCommitMail       string        `env:"SSH_COMMIT_MAIL" envDefault:"argo-watcher@example.com"`
	CommitMessageFormat string        `env:"COMMIT_MESSAGE_FORMAT"`
	// GitTimeout bounds the entire git update flow for a single task (clone +
	// fetch + commit + push, including any race-recovery retry). It is a total
	// wall-clock budget, not a per-operation timeout — this guarantees one task
	// cannot hold the per-repo lock for longer than this duration, so the
	// deployment queue cannot be wedged by a slow or hung remote.
	GitTimeout          time.Duration `env:"GIT_TIMEOUT" envDefault:"3m"`
}

func NewGitConfig() (*GitConfig, error) {
	var config GitConfig
	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}
	if config.GitTimeout <= 0 {
		return nil, fmt.Errorf("GIT_TIMEOUT must be > 0, got %s", config.GitTimeout)
	}
	return &config, nil
}
