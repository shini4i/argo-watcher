package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/shini4i/argo-watcher/pkg/updater"
	updatrmock "github.com/shini4i/argo-watcher/pkg/updater/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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

	t.Run("Rollout message - ArgoRolloutAppNotAvailable with failed hook", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{
				HookPhase: "Failed",
				HookType:  "PreSync",
				Kind:      "Job",
				Message:   "Job has reached the specified backoff limit",
				Status:    "Synced",
				SyncPhase: "PreSync",
				Name:      "app-migrations",
				Namespace: "app",
			},
		}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Failed hooks:\n"+
				"\tJob(app-migrations) PreSync Failed with message Job has reached the specified backoff limit",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images))
	})

	t.Run("Rollout message - ArgoRolloutAppNotAvailable with unhealthy pod", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.Resources = []ApplicationResource{
			{
				Kind:      "Pod",
				Name:      "app-pod",
				Namespace: "app",
				Health: struct {
					Message string `json:"message"`
					Status  string `json:"status"`
				}{
					Message: "Back-off restarting failed container",
					Status:  "Degraded",
				},
			},
		}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-pod) Degraded with message Back-off restarting failed container",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images))
	})

	t.Run("Rollout message - ArgoRolloutAppNotAvailable with failed sync operation", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.OperationState.Phase = "Failed"
		application.Status.OperationState.Message = "one or more synchronization tasks completed unsuccessfully"
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Sync operation phase: Failed\n"+
				"Sync operation message: one or more synchronization tasks completed unsuccessfully",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images))
	})

	t.Run("Rollout message - ArgoRolloutAppNotAvailable with combined diagnostics", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.OperationState.Phase = "Failed"
		application.Status.OperationState.Message = "one or more synchronization tasks completed unsuccessfully"
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{
				HookPhase: "Succeeded",
				HookType:  "PreSync",
				Kind:      "Pod",
				Status:    "Synced",
				SyncPhase: "PreSync",
				Name:      "ok-hook",
				Namespace: "app",
			},
			{
				HookPhase: "Failed",
				HookType:  "PreSync",
				Kind:      "Job",
				Message:   "Job has reached the specified backoff limit",
				Status:    "Synced",
				SyncPhase: "PreSync",
				Name:      "app-migrations",
				Namespace: "app",
			},
		}
		application.Status.Resources = []ApplicationResource{
			{
				Kind:      "Pod",
				Name:      "app-pod",
				Namespace: "app",
				Health: struct {
					Message string `json:"message"`
					Status  string `json:"status"`
				}{
					Message: "Back-off restarting failed container",
					Status:  "Degraded",
				},
			},
		}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Sync operation phase: Failed\n"+
				"Sync operation message: one or more synchronization tasks completed unsuccessfully\n\n"+
				"Failed hooks:\n"+
				"\tJob(app-migrations) PreSync Failed with message Job has reached the specified backoff limit\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-pod) Degraded with message Back-off restarting failed container",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images))
	})

	t.Run("Rollout message - ArgoRolloutAppNotAvailable filters out healthy/successful resources", func(t *testing.T) {
		// Successful hooks, healthy/empty-status pods, and a non-failed sync operation phase must NOT appear in
		// the failure report. We probe both `Running` (mid-rollout) and `Succeeded` (steady-state) op phases to
		// pin the filter to {Failed, Error} only.
		runCase := func(phase string) {
			application := Application{}
			application.Status.Summary.Images = []string{"app:v0.0.1"}
			application.Status.OperationState.Phase = phase
			application.Status.OperationState.Message = "irrelevant for this case"
			application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
				{HookPhase: "Succeeded", HookType: "PreSync", Kind: "Pod", Name: "ok", Namespace: "app"},
			}
			application.Status.Resources = []ApplicationResource{
				{
					Kind: "Pod", Name: "ok-pod", Namespace: "app",
					Health: struct {
						Message string `json:"message"`
						Status  string `json:"status"`
					}{Status: "Healthy"},
				},
				{
					Kind: "Pod", Name: "progressing-pod", Namespace: "app",
					Health: struct {
						Message string `json:"message"`
						Status  string `json:"status"`
					}{Status: "Progressing"},
				},
				{
					Kind: "Service", Name: "no-status-resource", Namespace: "app",
					Health: struct {
						Message string `json:"message"`
						Status  string `json:"status"`
					}{Status: ""},
				},
			}
			images := []string{"app:v0.0.2"}
			assert.Equal(t,
				"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
					"List of expected images:\n\tapp:v0.0.2",
				application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images),
				"phase=%q must produce the baseline image-only message", phase)
		}
		runCase("Running")
		runCase("Succeeded")
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

	t.Run("Rollout message - ArgoRolloutAppNotAvailable with failed sync operation and empty message", func(t *testing.T) {
		// Pins the OperationState.Message == "" branch in buildNotAvailableDiagnostics: the "Sync operation message:"
		// line must be omitted when ArgoCD provides only a phase.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.OperationState.Phase = "Error"
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Sync operation phase: Error",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images))
	})
}

func TestIsTerminalFailurePhase(t *testing.T) {
	// Pins the {Failed, Error} predicate behaviourally. Without these, the rollout-message tests cover the predicate
	// only structurally — narrowing the filter (e.g. dropping "Error") would not fail any other test.
	tests := []struct {
		phase string
		want  bool
	}{
		{"Failed", true},
		{"Error", true},
		{"Running", false},
		{"Succeeded", false},
		{"Terminating", false},
		{"", false},
		{"unknown-phase", false},
	}
	for _, tc := range tests {
		t.Run(tc.phase, func(t *testing.T) {
			assert.Equal(t, tc.want, isTerminalFailurePhase(tc.phase))
		})
	}
}

func TestIsProblemHealthStatus(t *testing.T) {
	// Pins the {Degraded, Missing, Unknown, Suspended} predicate behaviourally. Without these, the rollout-message
	// tests cover only "Degraded" — silently narrowing the filter would not fail any other test.
	tests := []struct {
		status string
		want   bool
	}{
		{"Degraded", true},
		{"Missing", true},
		{"Unknown", true},
		{"Suspended", true},
		{"Healthy", false},
		{"Progressing", false},
		{"", false},
		{"Synced", false},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			assert.Equal(t, tc.want, isProblemHealthStatus(tc.status))
		})
	}
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

func TestIsFireAndForgetModeActive(t *testing.T) {
	tt := []struct {
		name string
		app  Application
		want bool
	}{
		{
			name: "FireAndForget mode active",
			app: Application{
				Metadata: ApplicationMetadata{
					Annotations: map[string]string{
						fireAndForgetAnnotation: "true",
					},
				},
			},
			want: true,
		},
		{
			name: "FireAndForget mode inactive",
			app: Application{
				Metadata: ApplicationMetadata{
					Annotations: map[string]string{
						fireAndForgetAnnotation: "false",
					},
				},
			},
			want: false,
		},
		{
			name: "Annotations are nil",
			app: Application{
				Metadata: ApplicationMetadata{
					Annotations: nil,
				},
			},
			want: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.app.IsFireAndForgetModeActive())
		})
	}
}

// newAppWithImages builds an Application with managed-image annotations for testing.
func newAppWithImages(name string) *Application {
	return &Application{
		Metadata: ApplicationMetadata{
			Name: name,
			Annotations: map[string]string{
				"argo-watcher/managed-images":     "app=myimage",
				"argo-watcher/app.helm.image-tag": "image.tag",
			},
		},
	}
}

// newImageTask builds a Task with a single image for testing.
func newImageTask() *Task {
	return &Task{
		Id: "test-id",
		Images: []Image{
			{Image: "myimage", Tag: "v1.0.0"},
		},
	}
}

// computeRepoCachePath replicates the deterministic hash used by GitRepo.getRepoCachePath
// so tests can pre-create the cache directory at the path the code will actually use.
func computeRepoCachePath(base, repoURL, branch string) string {
	hasher := fnv.New64a()
	_, _ = io.WriteString(hasher, fmt.Sprintf("%s-%s", repoURL, branch))
	return filepath.Join(base, strconv.FormatUint(hasher.Sum64(), 16))
}

func TestUpdateGitImageTag(t *testing.T) {
	t.Run("Returns nil when path is empty", func(t *testing.T) {
		app := &Application{}
		task := &Task{Id: "test-id"}
		gitopsRepo := &GitopsRepo{Path: ""}

		err := app.UpdateGitImageTag(task, gitopsRepo, nil)
		assert.NoError(t, err)
	})

	t.Run("Returns nil when no release overrides found", func(t *testing.T) {
		app := &Application{
			Metadata: ApplicationMetadata{
				Name:        "test-app",
				Annotations: map[string]string{},
			},
		}
		task := &Task{Id: "test-id"}
		gitopsRepo := &GitopsRepo{Path: "/some/path"}

		err := app.UpdateGitImageTag(task, gitopsRepo, nil)
		assert.NoError(t, err)
	})

	t.Run("Returns error when NewGitRepo fails (no SSH_KEY_PATH)", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "")
		// caarlos0/env treats empty-string the same as unset for required fields.
		os.Unsetenv("SSH_KEY_PATH")

		err := newAppWithImages("test-app").UpdateGitImageTag(
			newImageTask(),
			&GitopsRepo{RepoUrl: "git@github.com:test/repo.git", BranchName: "main", Path: "/some/path"},
			updater.GitClient{},
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load git config")
	})

	t.Run("Returns error when Clone fails", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")

		ctrl := gomock.NewController(t)
		mockHandler := updatrmock.NewMockGitHandler(ctrl)
		mockHandler.EXPECT().
			AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("SSH key load failed"))

		err := newAppWithImages("test-app").UpdateGitImageTag(
			newImageTask(),
			&GitopsRepo{RepoUrl: "git@github.com:test/repo.git", BranchName: "main", Path: "/some/path", RepoCachePath: t.TempDir()},
			mockHandler,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SSH key load failed")
	})

	t.Run("Network calls receive a bounded context within GIT_TIMEOUT", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")
		t.Setenv("GIT_TIMEOUT", "1s")

		ctrl := gomock.NewController(t)
		mockHandler := updatrmock.NewMockGitHandler(ctrl)
		mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, gogit.ErrRepositoryNotExists)

		var capturedCtx context.Context
		mockHandler.EXPECT().
			PlainClone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ string, _ bool, _ *gogit.CloneOptions) (*gogit.Repository, error) {
				capturedCtx = ctx
				return nil, errors.New("clone failed")
			})

		_ = newAppWithImages("test-app").UpdateGitImageTag(
			newImageTask(),
			&GitopsRepo{RepoUrl: "git@github.com:test/repo.git", BranchName: "main", Path: "/some/path", RepoCachePath: t.TempDir()},
			mockHandler,
		)

		require.NotNil(t, capturedCtx, "PlainClone must receive a context")
		deadline, ok := capturedCtx.Deadline()
		require.True(t, ok, "PlainClone context must carry a deadline")
		remaining := time.Until(deadline)
		assert.Greater(t, remaining, time.Duration(0), "deadline must not be already expired")
		assert.LessOrEqual(t, remaining, 1*time.Second, "deadline must be within GIT_TIMEOUT (1s)")
	})

	t.Run("Recovery succeeds on push race", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")
		t.Setenv("GIT_TIMEOUT", "30s")

		repoCachePath := t.TempDir()
		const branchName = "master"
		const repoURL = "git@example.com:test/recovery-race.git"

		// 1. Create a bare remote with an initial empty commit.
		remotePath := t.TempDir()
		sourcePath := t.TempDir()
		sourceRepo, err := gogit.PlainInit(sourcePath, false)
		require.NoError(t, err)
		sourceWt, err := sourceRepo.Worktree()
		require.NoError(t, err)
		// Commit the apps directory so it survives the hard reset during recovery.
		require.NoError(t, os.MkdirAll(filepath.Join(sourcePath, "apps"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourcePath, "apps", ".gitkeep"), nil, 0644))
		_, err = sourceWt.Add("apps/.gitkeep")
		require.NoError(t, err)
		_, err = sourceWt.Commit("initial", &gogit.CommitOptions{
			Author: &object.Signature{Name: "init", Email: "init@test.com", When: time.Now()},
		})
		require.NoError(t, err)
		_, err = gogit.PlainClone(remotePath, true, &gogit.CloneOptions{URL: sourcePath})
		require.NoError(t, err)

		// 2. Clone into the exact path the code will use (local is at commit A).
		localRepoPath := computeRepoCachePath(repoCachePath, repoURL, branchName)
		localRepo, err := gogit.PlainClone(localRepoPath, false, &gogit.CloneOptions{
			URL: remotePath, ReferenceName: "refs/heads/master", SingleBranch: true,
		})
		require.NoError(t, err)

		// 3. Advance the remote to commit B so the first push (from A) will fail.
		competitorPath := t.TempDir()
		competitor, err := gogit.PlainClone(competitorPath, false, &gogit.CloneOptions{URL: remotePath})
		require.NoError(t, err)
		competitorWt, err := competitor.Worktree()
		require.NoError(t, err)
		_, err = competitorWt.Commit("competing commit", &gogit.CommitOptions{
			Author:            &object.Signature{Name: "other", Email: "other@test.com", When: time.Now()},
			AllowEmptyCommits: true,
		})
		require.NoError(t, err)
		require.NoError(t, competitor.Push(&gogit.PushOptions{}))

		// 4. Set up mock so the first Clone returns our stale localRepo (at commit A).
		//    The recovery Clone uses PlainOpen to find it on disk at localRepoPath, then
		//    FetchContext advances it to commit B, and the second UpdateApp succeeds.
		ctrl := gomock.NewController(t)
		mockHandler := updatrmock.NewMockGitHandler(ctrl)
		mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil).AnyTimes()
		first := mockHandler.EXPECT().PlainOpen(gomock.Any()).
			Return(nil, gogit.ErrRepositoryNotExists)
		mockHandler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(localRepo, nil)
		mockHandler.EXPECT().PlainOpen(gomock.Any()).
			Return(localRepo, nil).After(first)

		// 5. Run UpdateGitImageTag — should detect the push race, recover, and succeed.
		err = newAppWithImages("test-app").UpdateGitImageTag(
			newImageTask(),
			&GitopsRepo{
				RepoUrl:       repoURL,
				BranchName:    branchName,
				Path:          "apps",
				RepoCachePath: repoCachePath,
			},
			mockHandler,
		)
		assert.NoError(t, err)
	})
}
