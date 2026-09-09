package argocd

import (
	"slices"
	"strings"
)

// imagesContains reports whether images contains image. When a registry proxy is
// set it matches the image both with and without the proxy prefix.
func imagesContains(images []string, image string, registryProxy string) bool {
	if registryProxy != "" {
		imageWithProxy := registryProxy + "/" + image
		// We need to check image with and without proxy because mutating webhook
		// might not have finished image copy during first rollout part. (due to 30s timeout)
		return slices.Contains(images, image) || slices.Contains(images, imageWithProxy)
	} else {
		return slices.Contains(images, image)
	}
}

// imageName returns the repository part of a container image reference, with any
// tag and digest removed ("registry:5000/team/app:v1" -> "registry:5000/team/app").
// A colon only introduces a tag when it appears after the last path separator, so a
// registry host's port is preserved.
func imageName(reference string) string {
	name, _, _ := strings.Cut(reference, "@")

	if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
		name = name[:colon]
	}

	return name
}

// normalizeImages returns a sorted copy, leaving the original untouched.
func normalizeImages(images []string) []string {
	copied := append([]string(nil), images...)
	slices.Sort(copied)
	return copied
}
