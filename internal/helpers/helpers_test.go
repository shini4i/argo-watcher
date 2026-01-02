package helpers

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	image1        = "app:v0.0.1"
	image2        = "nginx:1.21.6"
	image3        = "migrations:v0.0.1"
	registryProxy = "registry.example.local"
)

type imagesContainsTest struct {
	images        []string
	image         string
	registryProxy string
	expected      bool
}

var imageContainsTest = []imagesContainsTest{
	{[]string{image1, image2, image3}, image1, "", true},
	{[]string{image1, image2, image3}, "nginx:1.21.7", "", false},
	{[]string{fmt.Sprintf("%s/%s", registryProxy, image1), image2, image3}, image1, registryProxy, true},
	{[]string{image1, image2, image3}, image1, registryProxy, true},
	{[]string{image1, image2, image3}, "v0.0.2", registryProxy, false},
}

// TestImageContains verifies that ImagesContains correctly detects images in a list,
// including scenarios with and without a registry proxy.
func TestImageContains(t *testing.T) {
	for _, test := range imageContainsTest {
		testErrorMsg := fmt.Sprintf("ImageContains(%s, %s, %s) should be %t", test.images, test.image, test.registryProxy, test.expected)
		assert.Equal(t, test.expected, ImagesContains(test.images, test.image, test.registryProxy), testErrorMsg)
	}
}

// TestCurlCommandFromRequest verifies that CurlCommandFromRequest generates
// a valid cURL command from an HTTP request with headers and body.
func TestCurlCommandFromRequest(t *testing.T) {
	// Create a sample HTTP request with a non-empty request body
	requestBody := `{"key": "value"}`
	request, _ := http.NewRequest("POST", "https://example.com/api", strings.NewReader(requestBody))
	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Authorization", "Bearer Token123")
	request.Header.Add("X-Custom-Header", "CustomValue")

	// Create the expected cURL command
	expectedCurl := `curl -X POST -H 'Authorization: Bearer Token123' -H 'Content-Type: application/json' -H 'X-Custom-Header: CustomValue' -d '{"key": "value"}' 'https://example.com/api'`

	// Call the function to get the actual cURL command
	actualCurl, err := CurlCommandFromRequest(request)
	assert.NoError(t, err)

	// Split the cURL commands by space
	expectedParts := strings.Fields(expectedCurl)
	actualParts := strings.Fields(actualCurl)

	// Sort the headers alphabetically, excluding the first part (curl command and method)
	sort.Strings(expectedParts[3:])
	sort.Strings(actualParts[3:])

	// Reconstruct the cURL commands with sorted headers
	sortedExpectedCurl := strings.Join(expectedParts, " ")
	sortedActualCurl := strings.Join(actualParts, " ")

	// Compare the expected and actual cURL commands
	assert.Equal(t, sortedExpectedCurl, sortedActualCurl)
}

// TestCurlCommandFromRequest_ShellEscaping verifies that single quotes in headers,
// body, and URL are properly escaped to prevent shell injection.
func TestCurlCommandFromRequest_ShellEscaping(t *testing.T) {
	// Create a sample HTTP request with single quotes in various places
	requestBody := `{"name": "O'Brien"}`
	request, _ := http.NewRequest("POST", "https://example.com/api?name=O'Connor", strings.NewReader(requestBody))
	request.Header.Add("X-Author", "O'Reilly")

	// Call the function to get the actual cURL command
	actualCurl, err := CurlCommandFromRequest(request)
	assert.NoError(t, err)

	// Verify single quotes are escaped with '\'' pattern
	assert.Contains(t, actualCurl, `O'\''Reilly`, "header value should have escaped single quote")
	assert.Contains(t, actualCurl, `O'\''Brien`, "body should have escaped single quote")
	assert.Contains(t, actualCurl, `O'\''Connor`, "URL should have escaped single quote")
}

// TestShellEscapeSingleQuote verifies that shellEscapeSingleQuote correctly escapes
// single quotes using the '\'' pattern for safe shell string interpolation.
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

// TestGenerateHash verifies that GenerateHash produces correct SHA256 hashes
// for known input strings.
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
