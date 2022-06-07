package helpers

import "os"

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func Contains(apps []string, app string) bool {
	for _, a := range apps {
		if a == app {
			return true
		}
	}
	return false
}
