package helpers

import (
	"os"
)

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func Contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func ImageContains(images []string, image string, registryProxy string) bool {
	if registryProxy != "" {
		imageWithProxy := registryProxy + "/" + image
		// We need to check image with and without proxy because mutating webhook
		// might not have finished image copy during first rollout part. (due to 30s timeout)
		return Contains(images, image) || Contains(images, imageWithProxy)
	} else {
		return Contains(images, image)
	}
}
