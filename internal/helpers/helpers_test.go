package helpers

import (
	"github.com/stretchr/testify/assert"
	"testing"
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
		{[]string{"app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}, "app:v0.0.1", "", true},
		{[]string{"app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}, "nginx:1.21.7", "", false},
		{[]string{"registry.example.local/app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}, "app:v0.0.1", "registry.example.local", true},
		{[]string{"app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}, "app:v0.0.1", "registry.example.local", true},
	}
)

func TestContains(t *testing.T) {
	for _, test := range containsTestSuite {
		assert.Equal(t, test.expected, Contains(test.strs, test.substr))
	}
}

func TestImageContains(t *testing.T) {
	for _, test := range imageContainsTest {
		assert.Equal(t, test.expected, ImagesContains(test.images, test.image, test.registryProxy))
	}
}
