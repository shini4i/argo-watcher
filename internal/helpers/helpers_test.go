package helpers

import (
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

type containsTest struct {
	strs     []string
	substr   string
	expected bool
}

type imagesContainsTest struct {
	images        []string
	image         string
	registryProxy string
	expected      bool
}

var (
	containsTestSuite = []containsTest{
		{[]string{"app1", "app2", "app3"}, "app1", true},
		{[]string{"app1", "app2", "app3"}, "app2", true},
		{[]string{"app1", "app2", "app3"}, "app3", true},
		{[]string{"app1", "app2", "app3"}, "app4", false},
	}

	imageContainsTest = []imagesContainsTest{
		{[]string{image1, image2, image3}, image1, "", true},
		{[]string{image1, image2, image3}, "nginx:1.21.7", "", false},
		{[]string{fmt.Sprintf("%s/%s", registryProxy, image1), image2, image3}, image1, registryProxy, true},
		{[]string{image1, image2, image3}, image1, registryProxy, true},
		{[]string{image1, image2, image3}, "v0.0.2", registryProxy, false},
	}
)

func TestContains(t *testing.T) {
	for _, test := range containsTestSuite {
		testErrorMsg := fmt.Sprintf("Contains(%s, %s) should be %t", test.strs, test.substr, test.expected)
		assert.Equal(t, test.expected, Contains(test.strs, test.substr), testErrorMsg)
	}
}

func TestImageContains(t *testing.T) {
	for _, test := range imageContainsTest {
		testErrorMsg := fmt.Sprintf("ImageContains(%s, %s, %s) should be %t", test.images, test.image, test.registryProxy, test.expected)
		assert.Equal(t, test.expected, ImagesContains(test.images, test.image, test.registryProxy), testErrorMsg)
	}
}

func TestCurlCommandFromRequest(t *testing.T) {
	// Create a sample HTTP request
	request, _ := http.NewRequest("POST", "https://example.com/api", nil)
	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Authorization", "Bearer Token123")
	request.Header.Add("X-Custom-Header", "CustomValue")

	// Create the expected cURL command
	expectedCurl := `curl -X POST -H 'Authorization: Bearer Token123' -H 'Content-Type: application/json' -H 'X-Custom-Header: CustomValue' 'https://example.com/api'`

	// Call the function to get the actual cURL command
	actualCurl := CurlCommandFromRequest(request)

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
