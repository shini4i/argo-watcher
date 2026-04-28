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

func TestUpdateGitImageTag(t *testing.T) {
	t.Run("Returns nil when path is empty", func(t *testing.T) {
		app := &Application{}
		task := &Task{Id: "test-id"}
		gitopsRepo := &GitopsRepo{Path: ""}

		err := app.UpdateGitImageTag(task, gitopsRepo)
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

		err := app.UpdateGitImageTag(task, gitopsRepo)
		assert.NoError(t, err)
	})

	t.Run("Returns error when NewGitRepo fails", func(t *testing.T) {
		// Ensure SSH_KEY_PATH is not set so NewGitRepo fails
		originalSSHKeyPath, wasSet := os.LookupEnv("SSH_KEY_PATH")
		os.Unsetenv("SSH_KEY_PATH")
		defer func() {
			if wasSet {
				os.Setenv("SSH_KEY_PATH", originalSSHKeyPath)
			}
		}()

		app := &Application{
			Metadata: ApplicationMetadata{
				Name: "test-app",
				Annotations: map[string]string{
					"argo-watcher/managed-images":         "app=myimage",
					"argo-watcher/app.helm.image-tag":     "image.tag",
				},
			},
		}
		task := &Task{
			Id: "test-id",
			Images: []Image{
				{Image: "myimage", Tag: "v1.0.0"},
			},
		}
		gitopsRepo := &GitopsRepo{
			RepoUrl:    "git@github.com:test/repo.git",
			BranchName: "main",
			Path:       "/some/path",
		}

		err := app.UpdateGitImageTag(task, gitopsRepo)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load git config")
	})
}
