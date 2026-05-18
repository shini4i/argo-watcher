package updater

import (
	"fmt"
	"strings"
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
	GitTimeout time.Duration `env:"GIT_TIMEOUT" envDefault:"3m"`
	// ExtraPushRaceMarkers are operator-supplied substrings appended to the
	// built-in pushRaceMarkers list. Comma-separated in the env var; trimmed
	// and lowercased at parse time so IsPushRaceError's case-insensitive
	// substring match works without per-call normalization. Additive only —
	// the built-in list cannot be replaced or disabled from configuration.
	ExtraPushRaceMarkers []string `env:"EXTRA_PUSH_RACE_MARKERS" envSeparator:","`
}

func NewGitConfig() (*GitConfig, error) {
	var config GitConfig
	if err := envConfig.Parse(&config); err != nil {
		return nil, err
	}
	if config.GitTimeout <= 0 {
		return nil, fmt.Errorf("GIT_TIMEOUT must be > 0, got %s", config.GitTimeout)
	}
	config.ExtraPushRaceMarkers = normalizeMarkers(config.ExtraPushRaceMarkers)
	return &config, nil
}

// normalizeMarkers lowercases, trims, and drops empty entries from the input.
// IsPushRaceError lowercases the error message but not the markers, so the
// markers must be lowercased at load time. Empty entries (e.g. from a trailing
// comma) would match every error, so they are filtered out.
func normalizeMarkers(in []string) []string {
	out := in[:0]
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}
