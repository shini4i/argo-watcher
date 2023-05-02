package helpers

import (
	"os"
	"strings"
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

func ImageContains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		} else {
			if strings.HasSuffix(item, "/"+s) {
				return true
			}
		}
	}
	return false
}
