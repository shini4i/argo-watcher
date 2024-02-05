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

func TestArgoRolloutStatus(t *testing.T) {
	t.Run("Rollout status - ArgoRolloutAppNotAvailable", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version2"}
		registryProxyUrl := ""
		// test status
		assert.Equal(t, ArgoRolloutAppNotAvailable, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppNotSynced", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Syncing"
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		// test status
		assert.Equal(t, ArgoRolloutAppNotSynced, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppNotHealthy", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "NotHealthy"
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		// test status
		assert.Equal(t, ArgoRolloutAppNotHealthy, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppSuccess", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Healthy"
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		// test status
		assert.Equal(t, ArgoRolloutAppSuccess, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppDegraded", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Synced" // Not "OutOfSync"
		application.Status.Health.Status = "Degraded"
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		// test status
		assert.Equal(t, ArgoRolloutAppDegraded, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("acceptSuspended is true", func(t *testing.T) {
		application := Application{}
		application.Status.Health.Status = "Suspended"
		application.Status.Sync.Status = "Synced"

		status := application.GetRolloutStatus([]string{}, "", true)
		if status != ArgoRolloutAppSuccess {
			t.Errorf("Expected status to be %s, but got %s", ArgoRolloutAppSuccess, status)
		}
	})

	t.Run("acceptSuspended is false", func(t *testing.T) {
		application := Application{}
		application.Status.Health.Status = "Suspended"
		application.Status.Sync.Status = "Synced"

		status := application.GetRolloutStatus([]string{}, "", false)
		if status == ArgoRolloutAppSuccess {
			t.Errorf("Expected status to not be %s", ArgoRolloutAppSuccess)
		}
	})
}

func TestArgoRolloutMessage(t *testing.T) {

	t.Run("Rollout message - ArgoRolloutAppNotAvailable", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tghcr.io/shini4i/argo-watcher:version1\n\nList of expected images:\n\tghcr.io/shini4i/argo-watcher:version2",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images))
	})

	t.Run("Rollout message - ArgoRolloutAppNotSynced", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Syncing"
		application.Status.Health.Status = "Healthy"
		application.Status.OperationState.Phase = "NotWorking"
		application.Status.OperationState.Message = "Not working test app"
		application.Status.OperationState.SyncResult.Resources = make([]ApplicationOperationResource, 2)
		// first resource
		application.Status.OperationState.SyncResult.Resources[0].HookPhase = "Succeeded"
		application.Status.OperationState.SyncResult.Resources[0].HookType = "PreSync"
		application.Status.OperationState.SyncResult.Resources[0].Kind = "Pod"
		application.Status.OperationState.SyncResult.Resources[0].Message = ""
		application.Status.OperationState.SyncResult.Resources[0].Status = "Synced"
		application.Status.OperationState.SyncResult.Resources[0].SyncPhase = "PreSync"
		application.Status.OperationState.SyncResult.Resources[0].Name = "app-migrations"
		application.Status.OperationState.SyncResult.Resources[0].Namespace = "app"
		// second resource
		application.Status.OperationState.SyncResult.Resources[1].HookPhase = "Failed"
		application.Status.OperationState.SyncResult.Resources[1].HookType = "PostSync"
		application.Status.OperationState.SyncResult.Resources[1].Kind = "Job"
		application.Status.OperationState.SyncResult.Resources[1].Message = "Job has reached the specified backoff limit"
		application.Status.OperationState.SyncResult.Resources[1].Status = "Synced"
		application.Status.OperationState.SyncResult.Resources[1].SyncPhase = "PostSync"
		application.Status.OperationState.SyncResult.Resources[1].Name = "app-job"
		application.Status.OperationState.SyncResult.Resources[1].Namespace = "app"
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		assert.Equal(t,
			"App status \"NotWorking\"\nApp message \"Not working test app\"\nResources:\n\tPod(app-migrations) PreSync Succeeded with message \n\tJob(app-job) PostSync Failed with message Job has reached the specified backoff limit",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, images))
	})

	t.Run("Rollout message - ArgoRolloutAppNotHealthy", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Syncing"
		application.Status.Health.Status = "NotHealthy"
		application.Status.OperationState.Phase = "NotWorking"
		application.Status.OperationState.Message = "Not working test app"
		application.Status.Resources = make([]ApplicationResource, 2)
		// first resource
		application.Status.Resources[0].Kind = "Pod"
		application.Status.Resources[0].Name = "app-pod"
		application.Status.Resources[0].Namespace = "app"
		application.Status.Resources[0].Health.Message = ""
		application.Status.Resources[0].Health.Status = "Synced"
		// second resource
		application.Status.Resources[1].Kind = "Job"
		application.Status.Resources[1].Name = "app-job"
		application.Status.Resources[1].Namespace = "app"
		application.Status.Resources[1].Health.Message = "Job has reached the specified backoff limit"
		application.Status.Resources[1].Health.Status = "Unhealthy"
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		assert.Equal(t,
			"App sync status \"Syncing\"\nApp health status \"NotHealthy\"\nResources:\n\tPod(app-pod) Synced\n\tJob(app-job) Unhealthy with message Job has reached the specified backoff limit",
			application.GetRolloutMessage(ArgoRolloutAppNotHealthy, images))
	})

	t.Run("Rollout message - default", func(t *testing.T) {
		// define application
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		// define expected images
		images := []string{"ghcr.io/shini4i/argo-watcher:version2"}
		assert.Equal(t, "received unexpected rollout status \"unexpected status\"", application.GetRolloutMessage("unexpected status", images))
	})
}

func TestIsManagedByWatcher(t *testing.T) {
	tests := []struct {
		name        string
		application Application
		expected    bool
	}{
		{
			name: "No annotations",
			application: Application{
				Metadata: struct {
					Name        string            `json:"name"`
					Annotations map[string]string `json:"annotations"`
				}{},
			},
			expected: false,
		},
		{
			name: "Managed by Watcher",
			application: Application{
				Metadata: struct {
					Name        string            `json:"name"`
					Annotations map[string]string `json:"annotations"`
				}{
					Annotations: map[string]string{
						managedAnnotation: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "Not managed by Watcher",
			application: Application{
				Metadata: struct {
					Name        string            `json:"name"`
					Annotations map[string]string `json:"annotations"`
				}{
					Annotations: map[string]string{
						managedAnnotation: "false",
					},
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.application.IsManagedByWatcher())
		})
	}
}

func TestExtractManagedImages(t *testing.T) {
	tests := []struct {
		name       string
		annotation map[string]string
		expected   map[string]string
		expectErr  bool
	}{
		{
			name: "Extracts multiple managed images",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1=image1,alias2=image2",
			},
			expected: map[string]string{
				"alias1": "image1",
				"alias2": "image2",
			},
		},
		{
			name: "Extracts multiple managed images with whitespace in alias",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1=image1, alias2=image2",
			},
			expected: map[string]string{
				"alias1": "image1",
				"alias2": "image2",
			},
		},
		{
			name: "Extracts multiple managed images with whitespace in image",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1=image1 ,alias2=image2",
			},
			expected: map[string]string{
				"alias1": "image1",
				"alias2": "image2",
			},
		},
		{
			name: "Extracts multiple managed images with whitespace in alias and image",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1=image1 , alias2=image2",
			},
			expected: map[string]string{
				"alias1": "image1",
				"alias2": "image2",
			},
		},
		{
			name: "Extracts single managed image",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1=image1",
			},
			expected: map[string]string{
				"alias1": "image1",
			},
		},
		{
			name:       "No managed images",
			annotation: map[string]string{},
			expected:   map[string]string{},
		},
		{
			name: "Non-related annotations",
			annotation: map[string]string{
				"another-annotation": "alias1=image1",
			},
			expected: map[string]string{},
		},
		{
			name: "Mixed annotations",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1=image1",
				"another-annotation":    "somethingelse",
			},
			expected: map[string]string{
				"alias1": "image1",
			},
		},
		{
			name: "Invalid format for managed images annotation",
			annotation: map[string]string{
				managedImagesAnnotation: "alias1image1",
			},
			expected:  nil,
			expectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extractedImages, err := extractManagedImages(test.annotation)
			if test.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expected, extractedImages)
			}
		})
	}
}
