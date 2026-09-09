package helpers

import (
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCurlCommandFromRequest(t *testing.T) {
	requestBody := `{"key": "value"}`
	request, _ := http.NewRequest("POST", "https://example.com/api", strings.NewReader(requestBody))
	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Authorization", "Bearer Token123")
	request.Header.Add("X-Custom-Header", "CustomValue")

	expectedCurl := `curl -X POST -H 'Authorization: Bearer Token123' -H 'Content-Type: application/json' -H 'X-Custom-Header: CustomValue' -d '{"key": "value"}' 'https://example.com/api'`

	actualCurl, err := CurlCommandFromRequest(request)
	assert.NoError(t, err)

	expectedParts := strings.Fields(expectedCurl)
	actualParts := strings.Fields(actualCurl)

	sort.Strings(expectedParts[3:])
	sort.Strings(actualParts[3:])

	sortedExpectedCurl := strings.Join(expectedParts, " ")
	sortedActualCurl := strings.Join(actualParts, " ")

	assert.Equal(t, sortedExpectedCurl, sortedActualCurl)
}

func TestCurlCommandFromRequest_RedactsHeaders(t *testing.T) {
	request, _ := http.NewRequest("POST", "https://example.com/api", strings.NewReader(""))
	request.Header.Add("Authorization", "super-secret-jwt")
	request.Header.Add("ARGO_WATCHER_DEPLOY_TOKEN", "super-secret-token")
	request.Header.Add("Content-Type", "application/json")

	actualCurl, err := CurlCommandFromRequest(request, "authorization", "ARGO_WATCHER_DEPLOY_TOKEN")
	assert.NoError(t, err)

	assert.NotContains(t, actualCurl, "super-secret-jwt", "JWT value must be redacted")
	assert.NotContains(t, actualCurl, "super-secret-token", "deploy token value must be redacted")

	assert.Contains(t, actualCurl, "-H 'Authorization: <redacted>'")
	assert.Contains(t, actualCurl, "-H 'Argo_watcher_deploy_token: <redacted>'")

	assert.Contains(t, actualCurl, "-H 'Content-Type: application/json'")
}

func TestCurlCommandFromRequest_RedactsMultiValueHeader(t *testing.T) {
	request, _ := http.NewRequest("POST", "https://example.com/api", strings.NewReader(""))
	request.Header.Add("Authorization", "secret-a")
	request.Header.Add("Authorization", "secret-b")

	actualCurl, err := CurlCommandFromRequest(request, "Authorization")
	assert.NoError(t, err)

	assert.NotContains(t, actualCurl, "secret-a")
	assert.NotContains(t, actualCurl, "secret-b")
	assert.Equal(t, 1, strings.Count(actualCurl, "-H 'Authorization: <redacted>'"),
		"multi-value sensitive header must collapse to exactly one redacted entry")
}

// Unescaped single quotes in a header, body or URL would be a shell-injection vector.
func TestCurlCommandFromRequest_ShellEscaping(t *testing.T) {
	requestBody := `{"name": "O'Brien"}`
	request, _ := http.NewRequest("POST", "https://example.com/api?name=O'Connor", strings.NewReader(requestBody))
	request.Header.Add("X-Author", "O'Reilly")

	actualCurl, err := CurlCommandFromRequest(request)
	assert.NoError(t, err)

	assert.Contains(t, actualCurl, `O'\''Reilly`, "header value should have escaped single quote")
	assert.Contains(t, actualCurl, `O'\''Brien`, "body should have escaped single quote")
	assert.Contains(t, actualCurl, `O'\''Connor`, "URL should have escaped single quote")
}

func TestShellEscapeSingleQuote(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"O'Brien", `O'\''Brien`},
		{"it's", `it'\''s`},
		{"'quoted'", `'\''quoted'\''`},
		{"no quotes", "no quotes"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := shellEscapeSingleQuote(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGenerateHash(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"world", "486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			hashBytes := GenerateHash(tc.input)
			hashString := hex.EncodeToString(hashBytes)
			assert.Equal(t, tc.expected, hashString)
		})
	}
}
