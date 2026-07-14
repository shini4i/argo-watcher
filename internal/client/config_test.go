package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// setValidClientEnv populates every required client env var with a valid value.
// Tests can override individual vars to exercise specific failure modes.
func setValidClientEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ARGO_WATCHER_URL", "http://localhost:8080")
	t.Setenv("IMAGES", "image1,image2")
	t.Setenv("IMAGE_TAG", "v1.0.0")
	t.Setenv("ARGO_APP", "test-app")
	t.Setenv("COMMIT_AUTHOR", "John Doe")
	t.Setenv("PROJECT_NAME", "test-project")
	t.Setenv("ARGO_WATCHER_DEPLOY_TOKEN", "test-token")
	t.Setenv("TIMEOUT", "60s")
	t.Setenv("DEBUG", "true")
}

func TestNewClientConfig_Success(t *testing.T) {
	setValidClientEnv(t)

	config, err := NewClientConfig()

	assert.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", config.Url)
	assert.Equal(t, []string{"image1", "image2"}, config.Images)
	assert.Equal(t, "v1.0.0", config.Tag)
	assert.Equal(t, "test-app", config.App)
	assert.Equal(t, "John Doe", config.Author)
	assert.Equal(t, "test-project", config.Project)
	assert.Equal(t, "test-token", config.Token)
	assert.Equal(t, 60*time.Second, config.Timeout)
	assert.Equal(t, true, config.Debug)
}

// TestNewClientConfig_InvalidDuration verifies that the formatter is wired
// in: bad TIMEOUT surfaces under the "invalid values" header so the user
// can spot the problem at a glance, instead of the env library's
// semicolon-joined one-liner.
func TestNewClientConfig_InvalidDuration(t *testing.T) {
	setValidClientEnv(t)
	t.Setenv("TIMEOUT", "invalid")

	_, err := NewClientConfig()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid values:")
	assert.Contains(t, err.Error(), "Timeout")
	assert.Contains(t, err.Error(), `"invalid"`)
}
