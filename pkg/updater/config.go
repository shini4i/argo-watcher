package updater

import (
	envConfig "github.com/caarlos0/env/v10"
)

type GitConfig struct {
	sshKeyPath          string `env:"SSH_KEY_PATH"`
	sshKeyPass          string `env:"SSH_KEY_PASS"`
	sshCommitUser       string `env:"SSH_COMMIT_USER"`
	sshCommitMail       string `env:"SSH_COMMIT_MAIL"`
	commitMessageFormat string `env:"COMMIT_MESSAGE_FORMAT"`
}

func NewGitConfig() (*GitConfig, error) {
	var config GitConfig

	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
