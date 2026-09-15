package argocd

import (
	"fmt"
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

func TestImageContains(t *testing.T) {
	for _, test := range imageContainsTest {
		testErrorMsg := fmt.Sprintf("ImageContains(%s, %s, %s) should be %t", test.images, test.image, test.registryProxy, test.expected)
		assert.Equal(t, test.expected, imagesContains(test.images, test.image, test.registryProxy), testErrorMsg)
	}
}

func TestImageName(t *testing.T) {
	tests := []struct {
		reference string
		expected  string
	}{
		{"nginx", "nginx"},
		{"nginx:1.21.6", "nginx"},
		{"ghcr.io/shini4i/argo-watcher:v0.13.0", "ghcr.io/shini4i/argo-watcher"},
		{"registry.example.local:5000/team/app:v1", "registry.example.local:5000/team/app"},
		{"registry.example.local:5000/team/app", "registry.example.local:5000/team/app"},
		{"ghcr.io/shini4i/argo-watcher@sha256:" + strings.Repeat("a", 64), "ghcr.io/shini4i/argo-watcher"},
		{"registry.example.local:5000/team/app:v1@sha256:" + strings.Repeat("b", 64), "registry.example.local:5000/team/app"},
		{"", ""},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, imageName(test.reference), "imageName(%q)", test.reference)
	}
}
