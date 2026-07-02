package helpers

import (
	"errors"
	"strings"
	"testing"
	"time"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
)

type sampleConfig struct {
	URL     string        `env:"SAMPLE_PRETTIFY_URL,required"`
	Name    string        `env:"SAMPLE_PRETTIFY_NAME,required"`
	Timeout time.Duration `env:"SAMPLE_PRETTIFY_TIMEOUT" envDefault:"30s"`
}

func TestPrettifyEnvError_PassThroughForNonAggregate(t *testing.T) {
	plain := errors.New("boom")
	out := PrettifyEnvError(plain, "lead-in:")
	// Non-AggregateError must come back unchanged so callers and tests can
	// rely on errors.Is/As against the original.
	assert.Same(t, plain, out)
}

func TestPrettifyEnvError_GroupsMissingAndInvalid(t *testing.T) {
	// SAMPLE_PRETTIFY_* are test-only fixture names guaranteed not to exist
	// in the ambient environment, so they trigger required-var errors
	// without unsetting anything. SAMPLE_PRETTIFY_TIMEOUT gets a bad value
	// so we also exercise the parse-error branch.
	t.Setenv("SAMPLE_PRETTIFY_TIMEOUT", "nope")

	_, parseErr := envConfig.ParseAs[sampleConfig]()
	assert.Error(t, parseErr)

	out := PrettifyEnvError(parseErr, "test config invalid:")
	msg := out.Error()

	assert.True(t, strings.HasPrefix(msg, "test config invalid:"))
	assert.Contains(t, msg, "missing required environment variables:")
	assert.Contains(t, msg, "  - SAMPLE_PRETTIFY_NAME")
	assert.Contains(t, msg, "  - SAMPLE_PRETTIFY_URL")
	assert.Contains(t, msg, "invalid values:")
	// The env library reports parse errors by Go field name, not env var
	// name; the helper passes that through verbatim. "Timeout" maps
	// obviously to SAMPLE_PRETTIFY_TIMEOUT for any operator.
	assert.Contains(t, msg, "Timeout")
	assert.Contains(t, msg, `"nope"`)
}

func TestPrettifyEnvError_InvalidOnlyOmitsMissingHeader(t *testing.T) {
	// All required vars are present, but one value fails to parse. The output
	// must report the invalid value without emitting an empty "missing
	// required environment variables:" header.
	t.Setenv("SAMPLE_PRETTIFY_URL", "http://example.com")
	t.Setenv("SAMPLE_PRETTIFY_NAME", "sample")
	t.Setenv("SAMPLE_PRETTIFY_TIMEOUT", "nope")

	_, parseErr := envConfig.ParseAs[sampleConfig]()
	assert.Error(t, parseErr)

	out := PrettifyEnvError(parseErr, "test config invalid:")
	msg := out.Error()

	assert.Contains(t, msg, "invalid values:")
	assert.Contains(t, msg, "Timeout")
	assert.NotContains(t, msg, "missing required environment variables:")
}
