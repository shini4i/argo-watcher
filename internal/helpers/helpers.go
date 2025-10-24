package helpers

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"slices"
	"strings"

	"crypto/sha256"
)

// Contains is a utility function that checks if a given item exists in a slice.
// It uses Go's generics to handle different types (string, int, etc.).
func Contains[T comparable](slice []T, item T) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// ImagesContains checks whether a given list of images contains a specific image.
// It takes into account an optional registry proxy and checks both the image with
// and without the proxy to ensure compatibility with mutating webhooks.
// The function returns true if the image is found in the list, considering the proxy if specified; otherwise, it returns false.
func ImagesContains(images []string, image string, registryProxy string) bool {
	if registryProxy != "" {
		imageWithProxy := registryProxy + "/" + image
		// We need to check image with and without proxy because mutating webhook
		// might not have finished image copy during first rollout part. (due to 30s timeout)
		return Contains(images, image) || Contains(images, imageWithProxy)
	} else {
		return Contains(images, image)
	}
}

// CurlCommandFromRequest generates a cURL command string from an HTTP request,
// including the request method, headers, request body, and URL.
// It handles any errors during the process and returns the formatted cURL command or an error if encountered.
func CurlCommandFromRequest(request *http.Request) (string, error) {
	clonedRequest, err := httputil.DumpRequest(request, true)
	if err != nil {
		return "", err
	}

	cmd := "curl -X " + request.Method

	// Iterate over request headers and add them to the cURL command
	for key, values := range request.Header {
		for _, value := range values {
			cmd += fmt.Sprintf(" -H '%s: %s'", key, value)
		}
	}

	// Add request body to cURL command
	if len(clonedRequest) > 0 {
		// Exclude the request line and headers when adding the body
		body := string(clonedRequest[strings.Index(string(clonedRequest), "\r\n\r\n")+4:])
		if len(body) > 0 {
			cmd += " -d '" + body + "'"
		}
	}

	// Add request URL to cURL command
	cmd += " '" + request.URL.String() + "'"

	return cmd, nil
}

// GenerateHash generates a SHA256 hash from a given string.
// It handles any errors during the process and returns the hash as []byte or an error if encountered.
func GenerateHash(s string) []byte {
	hash := sha256.New()
	// we intentionally ignore the error here because it will never return one
	// if you know a way to make this return an error, please open an issue
	hash.Write([]byte(s))
	return hash.Sum(nil)
}

// NormalizeImages returns a sorted copy of the provided image slice to guarantee stable ordering.
func NormalizeImages(images []string) []string {
	copied := append([]string(nil), images...)
	slices.Sort(copied)
	return copied
}
