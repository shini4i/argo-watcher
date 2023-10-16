package helpers

import (
	"fmt"
	"io"
	"net/http"
)

func Contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

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

func CurlCommandFromRequest(request *http.Request) string {
	cmd := "curl -X " + request.Method

	// Iterate over request headers and add them to the cURL command
	for key, values := range request.Header {
		for _, value := range values {
			cmd += fmt.Sprintf(" -H '%s: %s'", key, value)
		}
	}

	// Add request body to cURL command
	if request.Body != nil {
		body, _ := io.ReadAll(request.Body)
		cmd += " -d '" + string(body) + "'"
	}

	// Add request URL to cURL command
	cmd += " '" + request.URL.String() + "'"

	return cmd
}
