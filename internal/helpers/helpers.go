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

// ImageMatch Is a temporary setup to allow for a more flexible image matching
type ImageMatch func(images []string, image, registryProxy string) bool

func ImageContains(images []string, image string, registryProxy string) bool {
	for _, item := range images {
		if item == image {
			return true
		}
	}
	return false
}

func ImageContainsWithProxy(images []string, image string, registryProxy string) bool {
	imageWithProxy := registryProxy + "/" + image
	for _, item := range images {
		if item == image || item == imageWithProxy {
			return true
		}
	}
	return false
}
