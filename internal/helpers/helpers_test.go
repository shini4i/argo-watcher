package helpers

import (
	"os"
	"testing"
)

type varContainsTest struct {
	key, value string
	expected   bool
}
type containsTest struct {
	strs     []string
	substr   string
	expected bool
}

var (
	envVarContainsTest = []varContainsTest{
		{"USER", "ANY_VALUE", true},
		{"NOT_EXISTING_VARIABLE", "PRESENTED", true},
	}

	strContainsTest = []containsTest{
		{[]string{"app1", "app2", "app3"}, "app1", true},
		{[]string{"app1", "app2", "app3"}, "app2", true},
		{[]string{"app1", "app2", "app3"}, "app3", true},
		{[]string{"app1", "app2", "app3"}, "app4", false},
	}

	imageContainsTest = []containsTest{
		{[]string{"nginx:latest"}, "nginx:latest", true},
		{[]string{"nginx:lates"}, "nginx:latest", false},
		{[]string{"nginx:latest"}, "nginx:lates", false},
		{[]string{"ginx:latest"}, "nginx:latest", false},
		{[]string{"nginx:latest"}, "ginx:latest", false},
		{[]string{"custom-registry/nginx:latest"}, "nginx:latest", true},
		{[]string{"custom-registry/nginx:lates"}, "nginx:latest", false},
		{[]string{"custom-registry/nginx:latest"}, "nginx:lates", false},
		{[]string{"custom-registry/ginx:latest"}, "nginx:latest", false},
		{[]string{"custom-registry/nginx:latest"}, "ginx:latest", false},
	}
)

func TestGetEnv(t *testing.T) {
	for _, test := range envVarContainsTest {
		envVar := os.Getenv(test.key)
		if envVar == "" {
			if result := GetEnv(test.key, test.value); result != test.value {
				t.Errorf("Varaible %v value %v not equal to expected %v", test.key, result, test.value)
			}
		}
	}
}

func TestContains(t *testing.T) {
	for _, test := range strContainsTest {
		if result := Contains(test.strs, test.substr); result != test.expected {
			t.Errorf("Output %v not equal to expected %v with %v and %v", result, test.expected, test.strs, test.substr)
		}
	}
}
func TestImageContains(t *testing.T) {
	for _, test := range imageContainsTest {
		if result := ImagesContains(test.strs, test.substr, ""); result != test.expected {
			t.Errorf("Output %v not equal to expected %v with %v and %v", result, test.expected, test.strs, test.substr)
		}
	}
}
