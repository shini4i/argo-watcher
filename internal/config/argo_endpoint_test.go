package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// url.Parse accepts a bare host as a relative path, so a scheme-less ARGO_URL
// starts the server cleanly and then fails every call with "unsupported protocol
// scheme" — and the cookie jar drops the session token for it, since a jar keys
// cookies by scheme. Nothing in that failure names ARGO_URL as the cause.
func TestNewServerConfig_ArgoUrlNeedsAnAbsoluteHttpUrl(t *testing.T) {
	tests := []struct {
		name string
		url  string
		// want is the scheme and host the message echoes. The raw value is never
		// echoed: url.String() renders basic-auth userinfo, password included.
		want string
	}{
		{name: "no scheme", url: "argocd.example.com", want: `scheme "" and host ""`},
		{name: "scheme-relative", url: "//argocd.example.com", want: `scheme "" and host "argocd.example.com"`},
		{name: "a scheme the client cannot dial", url: "ftp://argocd.example.com", want: `scheme "ftp" and host "argocd.example.com"`},
		{name: "no host", url: "https://", want: `scheme "https" and host ""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ARGO_URL", tt.url)
			t.Setenv("ARGO_TOKEN", "secret-token")
			t.Setenv("STATE_TYPE", "in-memory")

			_, err := NewServerConfig()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "ArgoUrl")
			assert.Contains(t, err.Error(), tt.want, "the rejected parts must be named")
		})
	}
}

func TestNewServerConfig_ArgoUrlAcceptsHttpAndHttps(t *testing.T) {
	for _, argoUrl := range []string{"http://argocd.example.com", "https://argocd.example.com:8080/argo"} {
		t.Run(argoUrl, func(t *testing.T) {
			t.Setenv("ARGO_URL", argoUrl)
			t.Setenv("ARGO_TOKEN", "secret-token")
			t.Setenv("STATE_TYPE", "in-memory")

			_, err := NewServerConfig()
			assert.NoError(t, err)
		})
	}
}

// A non-positive timeout means "no timeout" to http.Client, so the initial
// application fetch — which runs on context.Background() — can hang forever while
// the lease guard keeps renewing the claim, leaving a task no sweep can resume.
func TestNewServerConfig_ArgoApiTimeoutMustBeInRange(t *testing.T) {
	// 3601 pins where the ceiling is; 9223372037 is where the nanosecond conversion
	// would overflow back into "no timeout".
	for _, timeout := range []string{"0", "-1", "3601", "9223372037"} {
		t.Run(timeout, func(t *testing.T) {
			t.Setenv("ARGO_URL", "https://example.com")
			t.Setenv("ARGO_TOKEN", "secret-token")
			t.Setenv("STATE_TYPE", "in-memory")
			t.Setenv("ARGO_API_TIMEOUT", timeout)

			_, err := NewServerConfig()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "ArgoApiTimeout")
			assert.Contains(t, err.Error(), timeout)
		})
	}
}

func TestNewServerConfig_ArgoApiTimeoutAcceptsTheWholeRange(t *testing.T) {
	for _, timeout := range []string{"1", "60", "3600"} {
		t.Run(timeout, func(t *testing.T) {
			t.Setenv("ARGO_URL", "https://example.com")
			t.Setenv("ARGO_TOKEN", "secret-token")
			t.Setenv("STATE_TYPE", "in-memory")
			t.Setenv("ARGO_API_TIMEOUT", timeout)

			_, err := NewServerConfig()
			assert.NoError(t, err)
		})
	}
}

// url.URL.String() renders basic-auth credentials, password included, and the
// startup error is logged. MarshalText already strips userinfo for the config
// endpoint; neither the validation message nor the parse error may reintroduce it.
func TestNewServerConfig_ArgoUrlRejectionKeepsCredentialsOut(t *testing.T) {
	tests := []struct {
		name string
		url  string
		// want is what the error must still say, so a rejection for some unrelated
		// reason cannot satisfy the redaction assertions on its own.
		want string
	}{
		{name: "unusable scheme", url: "gopher://admin:s3cret@argocd.example.com", want: "ArgoUrl"},
		{name: "username read as the scheme", url: "admin:s3cret@argocd.example.com", want: "ArgoUrl"},
		// Rejected by url.Parse itself, whose *url.Error quotes the whole value.
		{name: "url.Parse refuses it", url: "https://admin:s3cret @argocd.example.com", want: "invalid userinfo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ARGO_URL", tt.url)
			t.Setenv("ARGO_TOKEN", "secret-token")
			t.Setenv("STATE_TYPE", "in-memory")

			_, err := NewServerConfig()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want, "the error must name why the value was refused")
			assert.NotContains(t, err.Error(), "s3cret", "the password must never reach the error")
		})
	}
}
