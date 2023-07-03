package models

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListSyncResultResources(t *testing.T) {
	// Read the JSON data file
	jsonData, err := os.ReadFile("../../testdata/failed-deployment.json")
	if err != nil {
		t.Fatalf("Failed to read JSON data file: %s", err)
	}

	// Unmarshal JSON into the Application struct
	var app Application
	err = json.Unmarshal(jsonData, &app)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %s", err)
	}

	// Invoke the method being tested
	resultResources := app.ListSyncResultResources()

	// Define the expected resource strings based on the JSON data
	expectedResources := []string{
		"Pod(app-migrations) PreSync Succeeded with message ",
		"Job(app-job) PostSync Failed with message Job has reached the specified backoff limit",
	}

	// Assert that the result matches the expected resources
	assert.Equal(t, expectedResources, resultResources)
}

func TestListUnhealthyResources(t *testing.T) {
	// Read the JSON data file
	jsonData, err := os.ReadFile("../../testdata/failed-deployment.json")
	if err != nil {
		t.Fatalf("Failed to read JSON data file: %s", err)
	}

	// Unmarshal JSON into the Application struct
	var app Application
	err = json.Unmarshal(jsonData, &app)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %s", err)
	}

	// Invoke the method being tested
	unhealthyResources := app.ListUnhealthyResources()

	// Define the expected resource strings based on the JSON data
	expectedResources := []string{
		"Pod(app-pod) Synced",
		"Job(app-job) Unhealthy with message Job has reached the specified backoff limit",
	}

	// Assert that the result matches the expected unhealthy resources
	assert.Equal(t, expectedResources, unhealthyResources)
}
