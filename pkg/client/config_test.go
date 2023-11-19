package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewClientConfig(t *testing.T) {
	t.Setenv("ARGO_WATCHER_URL", "http://localhost:8080")
	t.Setenv("IMAGES", "image1,image2")
	t.Setenv("IMAGE_TAG", "v1.0.0")
	t.Setenv("ARGO_APP", "test-app")
	t.Setenv("COMMIT_AUTHOR", "John Doe")
	t.Setenv("PROJECT_NAME", "test-project")
	t.Setenv("ARGO_WATCHER_DEPLOY_TOKEN", "test-token")
	t.Setenv("TIMEOUT", "60s")
	t.Setenv("DEBUG", "true")

	t.Run("Successfully generated", func(t *testing.T) {
		// Call the function
		config, err := NewClientConfig()

		// Assert there was no error
		assert.NoError(t, err)

		// Assert the config was correctly populated
		assert.Equal(t, "http://localhost:8080", config.Url)
		assert.Equal(t, []string{"image1", "image2"}, config.Images)
		assert.Equal(t, "v1.0.0", config.Tag)
		assert.Equal(t, "test-app", config.App)
		assert.Equal(t, "John Doe", config.Author)
		assert.Equal(t, "test-project", config.Project)
		assert.Equal(t, "test-token", config.Token)
		assert.Equal(t, 60*time.Second, config.Timeout)
		assert.Equal(t, true, config.Debug)
	})

	t.Run("Failed to generate", func(t *testing.T) {
		// Set an invalid value for TIMEOUT
		t.Setenv("TIMEOUT", "invalid")

		// Call the function
		_, err := NewClientConfig()

		// Assert that an error was returned
		assert.Error(t, err)

		// Assert that the error is the expected one
		assert.Equal(t, "env: parse error on field \"Timeout\" of type \"time.Duration\": unable to parse duration: time: invalid duration \"invalid\"", err.Error())
	})
}
