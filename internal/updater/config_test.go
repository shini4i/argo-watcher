package updater

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitConfig(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("SSH_KEY_PASS", "test_pass")
		t.Setenv("SSH_COMMIT_USER", "test_user")
		t.Setenv("SSH_COMMIT_MAIL", "test@email.com")
		t.Setenv("COMMIT_MESSAGE_FORMAT", "test_format")
		t.Setenv("GIT_OP_TIMEOUT", "45s")
		t.Setenv("GIT_MAX_ATTEMPTS", "5")

		config, err := NewGitConfig()

		require.NoError(t, err)
		require.NotNil(t, config)

		assert.Equal(t, "/test/key", config.SshKeyPath)
		assert.Equal(t, "test_pass", config.SshKeyPass)
		assert.Equal(t, "test_user", config.SshCommitUser)
		assert.Equal(t, "test@email.com", config.SshCommitMail)
		assert.Equal(t, "test_format", config.CommitMessageFormat)
		assert.Equal(t, 45*time.Second, config.GitOpTimeout)
		assert.Equal(t, uint(5), config.GitMaxAttempts)
	})

	t.Run("GitOpTimeout defaults to 90s", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, 90*time.Second, config.GitOpTimeout)
	})

	t.Run("GitMaxAttempts defaults to 5", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, uint(5), config.GitMaxAttempts)
	})

	t.Run("Failure - Missing Required Env Var", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "")
		os.Unsetenv("SSH_KEY_PATH") //nolint:errcheck

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "missing required environment variables")
		assert.Contains(t, err.Error(), "SSH_KEY_PATH")
	})

	t.Run("Failure - Malformed GIT_OP_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_OP_TIMEOUT", "abc")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "duration")
	})

	t.Run("Failure - Zero GIT_OP_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_OP_TIMEOUT", "0s")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_OP_TIMEOUT")
	})

	t.Run("Failure - Negative GIT_OP_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_OP_TIMEOUT", "-1s")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_OP_TIMEOUT")
	})

	t.Run("Failure - Zero GIT_MAX_ATTEMPTS", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_MAX_ATTEMPTS", "0")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_MAX_ATTEMPTS")
	})
}

// TestLegacyGitTimeoutMapping covers the backward-compat shim that maps the
// deprecated GIT_TIMEOUT directly to GIT_OP_TIMEOUT (1:1, no division), so
// the per-attempt budget is unchanged for operators that have not migrated.
func TestLegacyGitTimeoutMapping(t *testing.T) {
	t.Run("GIT_TIMEOUT alone is used directly as GIT_OP_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_TIMEOUT", "3m")
		// GIT_OP_TIMEOUT intentionally unset. The 1:1 mapping preserves the old
		// per-call budget: GIT_OP_TIMEOUT = 3m. GIT_MAX_ATTEMPTS stays at its
		// default (5), so the worst-case total wall clock is 15m.

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, 3*time.Minute, config.GitOpTimeout)
	})

	t.Run("GIT_TIMEOUT mapping is independent of GIT_MAX_ATTEMPTS", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_TIMEOUT", "2m")
		t.Setenv("GIT_MAX_ATTEMPTS", "4")
		// Still a 1:1 map: GIT_OP_TIMEOUT = 2m.
		// GIT_MAX_ATTEMPTS only affects total wall-clock ceiling, not the per-attempt budget.

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, 2*time.Minute, config.GitOpTimeout)
	})

	t.Run("GIT_TIMEOUT is ignored when GIT_OP_TIMEOUT is set explicitly", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_TIMEOUT", "10m")
		t.Setenv("GIT_OP_TIMEOUT", "20s")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, 20*time.Second, config.GitOpTimeout, "GIT_OP_TIMEOUT must take precedence")
	})

	t.Run("Failure - Malformed legacy GIT_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_TIMEOUT", "not-a-duration")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_TIMEOUT")
	})

	t.Run("Failure - Zero legacy GIT_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("GIT_TIMEOUT", "0s")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_TIMEOUT")
	})
}
