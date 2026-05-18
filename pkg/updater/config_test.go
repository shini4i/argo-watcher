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
		// Arrange: Set all required environment variables for a successful parse.
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("SSH_KEY_PASS", "test_pass")
		t.Setenv("SSH_COMMIT_USER", "test_user")
		t.Setenv("SSH_COMMIT_MAIL", "test@email.com")
		t.Setenv("COMMIT_MESSAGE_FORMAT", "test_format")
		t.Setenv("GIT_TIMEOUT", "30s")

		// Act: Call the function to be tested.
		config, err := NewGitConfig()

		// Assert: Verify that no error occurred and the config is populated correctly.
		require.NoError(t, err)
		require.NotNil(t, config)

		assert.Equal(t, "/test/key", config.SshKeyPath)
		assert.Equal(t, "test_pass", config.SshKeyPass)
		assert.Equal(t, "test_user", config.SshCommitUser)
		assert.Equal(t, "test@email.com", config.SshCommitMail)
		assert.Equal(t, "test_format", config.CommitMessageFormat)
		assert.Equal(t, 30*time.Second, config.GitTimeout)
	})

	t.Run("GitTimeout defaults to 3m", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		// GIT_TIMEOUT intentionally unset — t.Setenv from sibling subtests is auto-cleared.

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, 3*time.Minute, config.GitTimeout)
	})

	t.Run("Failure - Missing Required Env Var", func(t *testing.T) {
		// Force the var absent regardless of what the parent process has set.
		t.Setenv("SSH_KEY_PATH", "")
		os.Unsetenv("SSH_KEY_PATH") //nolint:errcheck

		config, err := NewGitConfig()

		// Assert: Verify that an error is returned and the config is nil.
		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "required environment variable \"SSH_KEY_PATH\" is not set")
	})

	t.Run("Failure - Malformed GIT_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("SSH_KEY_PASS", "test_pass")
		t.Setenv("SSH_COMMIT_USER", "test_user")
		t.Setenv("SSH_COMMIT_MAIL", "test@email.com")
		t.Setenv("GIT_TIMEOUT", "abc")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "duration")
	})

	t.Run("Failure - Zero GIT_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("SSH_KEY_PASS", "test_pass")
		t.Setenv("SSH_COMMIT_USER", "test_user")
		t.Setenv("SSH_COMMIT_MAIL", "test@email.com")
		t.Setenv("GIT_TIMEOUT", "0s")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_TIMEOUT")
	})

	t.Run("Failure - Negative GIT_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("SSH_KEY_PASS", "test_pass")
		t.Setenv("SSH_COMMIT_USER", "test_user")
		t.Setenv("SSH_COMMIT_MAIL", "test@email.com")
		t.Setenv("GIT_TIMEOUT", "-1s")

		config, err := NewGitConfig()

		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "GIT_TIMEOUT")
	})

	t.Run("ExtraPushRaceMarkers - unset defaults to empty", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Empty(t, config.ExtraPushRaceMarkers)
	})

	t.Run("ExtraPushRaceMarkers - normalized (lowercase, trimmed, empties dropped)", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		// Mixed casing, leading/trailing whitespace, and a trailing comma that
		// produces an empty entry — all must be normalized away.
		t.Setenv("EXTRA_PUSH_RACE_MARKERS", "Foo,  BAR  ,,baz")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar", "baz"}, config.ExtraPushRaceMarkers)
	})

	t.Run("ExtraPushRaceMarkers - single value", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		t.Setenv("EXTRA_PUSH_RACE_MARKERS", "change conflicts")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Equal(t, []string{"change conflicts"}, config.ExtraPushRaceMarkers)
	})

	t.Run("ExtraPushRaceMarkers - empty value yields empty slice", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/test/key")
		// EXTRA_PUSH_RACE_MARKERS explicitly set to empty: caarlos0/env may
		// produce a single empty entry, which normalizeMarkers must drop.
		t.Setenv("EXTRA_PUSH_RACE_MARKERS", "")

		config, err := NewGitConfig()

		require.NoError(t, err)
		assert.Empty(t, config.ExtraPushRaceMarkers)
	})
}
