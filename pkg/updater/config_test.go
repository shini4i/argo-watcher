package updater

import (
	"testing"

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
	})

	t.Run("Failure - Missing Required Env Var", func(t *testing.T) {
		// Arrange: Intentionally do not set the required SSH_KEY_PATH.
		// t.Setenv from the previous sub-test is automatically cleared.

		// Act: Call the function.
		config, err := NewGitConfig()

		// Assert: Verify that an error is returned and the config is nil.
		require.Error(t, err)
		assert.Nil(t, config)
		assert.Contains(t, err.Error(), "required environment variable \"SSH_KEY_PATH\" is not set")
	})
}
