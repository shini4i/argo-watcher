package updater

import (
	envConfig "github.com/caarlos0/env/v10"
)

type GitConfig struct {
	SshKeyPath          string `env:"SSH_KEY_PATH"`
	SshKeyPass          string `env:"SSH_KEY_PASS"`
	SshCommitUser       string `env:"SSH_COMMIT_USER"`
	SshCommitMail       string `env:"SSH_COMMIT_MAIL"`
	CommitMessageFormat string `env:"COMMIT_MESSAGE_FORMAT"`
}

func NewGitConfig() (*GitConfig, error) {
	var config GitConfig

	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
