package updater

import (
	envConfig "github.com/caarlos0/env/v11"
)

type GitConfig struct {
	SshKeyPath          string `env:"SSH_KEY_PATH"`
	SshKeyPass          string `env:"SSH_KEY_PASS"`
	SshCommitUser       string `env:"SSH_COMMIT_USER"`
	SshCommitMail       string `env:"SSH_COMMIT_MAIL"`
	CommitMessageFormat string `env:"COMMIT_MESSAGE_FORMAT"`
}

func NewGitConfig() (*GitConfig, error) {
	var err error
	var config GitConfig

	if config, err = envConfig.ParseAs[GitConfig](); err != nil {
		return nil, err
	}

	return &config, nil
}
