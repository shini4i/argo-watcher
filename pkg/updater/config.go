package updater

import (
	"fmt"
	"os"
	"time"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/rs/zerolog/log"
)

// GitConfig holds runtime configuration for the git updater, parsed from
// environment variables on startup.
type GitConfig struct {
	SshKeyPath          string `env:"SSH_KEY_PATH,required"`
	SshKeyPass          string `env:"SSH_KEY_PASS"`
	SshCommitUser       string `env:"SSH_COMMIT_USER" envDefault:"argo-watcher"`
	SshCommitMail       string `env:"SSH_COMMIT_MAIL" envDefault:"argo-watcher@example.com"`
	CommitMessageFormat string `env:"COMMIT_MESSAGE_FORMAT"`
	// GitOpTimeout bounds a single clone+update attempt. Per-attempt (not total)
	// timeout is deliberate: it lets retries actually succeed when the first
	// attempt times out on a slow remote. The worst-case wall clock for the full
	// retry loop is GitOpTimeout * GitMaxAttempts plus (GitMaxAttempts-1) × 2s
	// inter-attempt backoff.
	GitOpTimeout time.Duration `env:"GIT_OP_TIMEOUT" envDefault:"90s"`
	// GitMaxAttempts is the total number of attempts (initial + retries) the
	// updater will make before giving up. On the final attempt the on-disk
	// cache is invalidated and a fresh clone is performed, so a poisoned cache
	// self-heals without operator intervention.
	GitMaxAttempts uint `env:"GIT_MAX_ATTEMPTS" envDefault:"3"`
}

// NewGitConfig loads GitConfig from environment variables and applies the
// backward-compat mapping for the deprecated GIT_TIMEOUT variable.
//
// When GIT_TIMEOUT is set and GIT_OP_TIMEOUT is not, the legacy value is
// used directly as GIT_OP_TIMEOUT (1:1 mapping) so the original per-call
// budget is preserved unchanged. A deprecation warning is logged in both
// cases (mapped or ignored). New deployments should set GIT_OP_TIMEOUT and
// GIT_MAX_ATTEMPTS directly.
func NewGitConfig() (*GitConfig, error) {
	var config GitConfig
	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}

	if err := applyLegacyGitTimeout(&config); err != nil {
		return nil, err
	}

	if config.GitOpTimeout <= 0 {
		return nil, fmt.Errorf("GIT_OP_TIMEOUT must be > 0, got %s", config.GitOpTimeout)
	}
	if config.GitMaxAttempts == 0 {
		return nil, fmt.Errorf("GIT_MAX_ATTEMPTS must be > 0")
	}

	return &config, nil
}

// applyLegacyGitTimeout maps the deprecated GIT_TIMEOUT env var to GIT_OP_TIMEOUT
// when the latter was not set explicitly. The mapping is 1:1 — GIT_TIMEOUT is used
// directly as GIT_OP_TIMEOUT — to preserve the original per-call budget unchanged.
// Retries are opt-in via GIT_MAX_ATTEMPTS; the old single-attempt wall clock is
// preserved per attempt rather than divided across retries.
func applyLegacyGitTimeout(config *GitConfig) error {
	legacyRaw, legacySet := os.LookupEnv("GIT_TIMEOUT")
	if !legacySet {
		return nil
	}

	legacy, err := time.ParseDuration(legacyRaw)
	if err != nil {
		return fmt.Errorf("GIT_TIMEOUT: invalid duration %q: %w", legacyRaw, err)
	}
	if legacy <= 0 {
		return fmt.Errorf("GIT_TIMEOUT must be > 0, got %s", legacy)
	}

	if _, newSet := os.LookupEnv("GIT_OP_TIMEOUT"); newSet {
		log.Warn().Msgf("GIT_TIMEOUT is deprecated and was ignored because GIT_OP_TIMEOUT is set. Remove GIT_TIMEOUT to silence this warning.")
		return nil
	}

	log.Warn().Msgf(
		"GIT_TIMEOUT is deprecated; using %s as GIT_OP_TIMEOUT directly. With GIT_MAX_ATTEMPTS=%d retries enabled, the worst-case total wall clock is %s. Set GIT_OP_TIMEOUT explicitly to silence this warning.",
		legacy, config.GitMaxAttempts, legacy*time.Duration(config.GitMaxAttempts),
	)
	config.GitOpTimeout = legacy
	return nil
}
