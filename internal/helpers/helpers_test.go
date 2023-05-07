package helpers

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
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
		assert.Equal(t, test.expected, Contains(test.strs, test.substr), "Contains(%s, %s) should be %t", test.strs, test.substr, test.expected)
	}
}

func TestImageContains(t *testing.T) {
	for _, test := range imageContainsTest {
		assert.Equal(t, test.expected, ImagesContains(test.images, test.image, test.registryProxy), "ImagesContains(%s, %s, %s) should be %t", test.images, test.image, test.registryProxy, test.expected)
	}
}
