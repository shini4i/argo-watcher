package models

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestListFailedSyncResultResources(t *testing.T) {
	jsonData, err := os.ReadFile("testdata/failed-deployment.json")
	if err != nil {
		t.Fatalf("Failed to read JSON data file: %s", err)
	}

	var app Application
	err = json.Unmarshal(jsonData, &app)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %s", err)
	}

	// The successful PreSync hook in the payload must not be listed: a failure report that
	// names resources which worked is what made these reports unreadable.
	expectedResources := []string{
		"Job(app-job) PostSync Failed with message Job has reached the specified backoff limit",
	}

	assert.Equal(t, expectedResources, app.listFailedSyncResultResources())
}

func TestFormatSyncResultResource(t *testing.T) {
	t.Run("an ordinary resource is reported by its sync status, not its hook phase", func(t *testing.T) {
		// gitops-engine sets HookPhase to "Running" for every successfully applied non-hook
		// resource, so printing the phase here made a clean apply look like a stalled one.
		assert.Equal(t,
			"Deployment(order-management) SyncFailed with message error validating data",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:      "Deployment",
				Name:      "order-management",
				Status:    "SyncFailed",
				HookPhase: "Failed",
				Message:   "error validating data",
			}))
	})

	t.Run("a hook is reported by its type and phase", func(t *testing.T) {
		assert.Equal(t,
			"Job(app-migrations) PreSync Failed with message backoff limit reached",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:      "Job",
				Name:      "app-migrations",
				Status:    "Synced",
				HookType:  "PreSync",
				HookPhase: "Failed",
				Message:   "backoff limit reached",
			}))
	})

	t.Run("an empty message leaves no dangling suffix", func(t *testing.T) {
		assert.Equal(t,
			"Job(app-migrations) PreSync Failed",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:      "Job",
				Name:      "app-migrations",
				HookType:  "PreSync",
				HookPhase: "Failed",
			}))
	})

	t.Run("a terminal phase outranks the sync status", func(t *testing.T) {
		// gitops-engine reports a resource whose live object went Degraded mid-sync as phase
		// "Failed" while leaving the sync status at "Synced". Printing the status there would
		// label the resource "Synced" inside the list of resources that failed.
		assert.Equal(t,
			"Deployment(order-management) Failed with message Deployment has 1 unavailable replica",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:      "Deployment",
				Name:      "order-management",
				Status:    "Synced",
				HookPhase: "Failed",
				Message:   "Deployment has 1 unavailable replica",
			}))
	})

	t.Run("a hook that failed its dry run has no phase to report", func(t *testing.T) {
		// That path records the sync status and leaves the phase empty, so the outcome has to come
		// from the status; rendering the empty phase left a blank where the failure should be.
		assert.Equal(t,
			"Job(app-migrations) PreSync SyncFailed with message error validating data",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:     "Job",
				Name:     "app-migrations",
				Status:   "SyncFailed",
				HookType: "PreSync",
				Message:  "error validating data",
			}))
	})

	t.Run("a clean apply is described by its status, not by the engine's Running phase", func(t *testing.T) {
		// The failure listing filters these out, so this shape reaches the formatter only from a
		// future caller — the formatter still must not call a successful apply "Running".
		assert.Equal(t,
			"Deployment(order-management) Synced with message deployment.apps/order-management configured",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:      "Deployment",
				Name:      "order-management",
				Status:    "Synced",
				HookPhase: "Running",
				Message:   "deployment.apps/order-management configured",
			}))
	})

	t.Run("with no status at all the phase is the only outcome left", func(t *testing.T) {
		assert.Equal(t,
			"Pod(app-hook) PreSync Running",
			formatSyncResultResource(ApplicationOperationResource{
				Kind:      "Pod",
				Name:      "app-hook",
				HookType:  "PreSync",
				HookPhase: "Running",
			}))
	})
}

func TestRolloutFailureHeadline(t *testing.T) {
	tests := []struct {
		name       string
		syncStatus string
		waited     time.Duration
		expected   string
	}{
		{
			"names the observed sync status and the time waited",
			"OutOfSync", 225 * time.Second,
			"Deployment failed: ArgoCD reports sync status OutOfSync after waiting 3m45s.",
		},
		{
			"omits the duration when it is unknown",
			"Unknown", 0,
			"Deployment failed: ArgoCD reports sync status Unknown.",
		},
		{
			// A payload ArgoCD has not compared yet carries no sync status, and "reports sync
			// status ." is the kind of half sentence this report exists to stop producing.
			"falls back to unknown when ArgoCD reported no sync status",
			"", 225 * time.Second,
			"Deployment failed: ArgoCD reports sync status unknown after waiting 3m45s.",
		},
		{
			// Anything under a second rounds to "0s", which reads as a bug, not a fast failure.
			"omits a sub-second duration rather than rounding it to zero",
			"OutOfSync", 400 * time.Millisecond,
			"Deployment failed: ArgoCD reports sync status OutOfSync.",
		},
		{
			"rounds the duration to whole seconds",
			"OutOfSync", 1500 * time.Millisecond,
			"Deployment failed: ArgoCD reports sync status OutOfSync after waiting 2s.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := Application{}
			application.Status.Sync.Status = test.syncStatus
			assert.Equal(t, test.expected, application.RolloutFailureHeadline(ArgoRolloutAppNotSynced, test.waited))
		})
	}

	t.Run("every other failure keeps the legacy headline", func(t *testing.T) {
		application := Application{}
		assert.Equal(t,
			"Application deployment failed. Rollout status is not available",
			application.RolloutFailureHeadline(ArgoRolloutAppNotAvailable, 225*time.Second))
	})
}

func TestShortRevision(t *testing.T) {
	tests := []struct {
		name     string
		revision string
		expected string
	}{
		{"a full SHA is abbreviated", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "a1b2c3d"},
		{"an uppercase SHA is abbreviated too", "A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F8A9B0", "A1B2C3D"},
		// Truncating a branch or chart reference would misname the revision in the report.
		{"a 40-character non-hex revision is left alone", "release/2026-08-19-hotfix-order-mgmt-xyz", "release/2026-08-19-hotfix-order-mgmt-xyz"},
		{"an already-short revision is left alone", "v1.2.3", "v1.2.3"},
		{"an empty revision stays empty", "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, shortRevision(test.revision))
		})
	}
}

func TestAutoSyncEnabled(t *testing.T) {
	enabled, disabled := true, false

	tests := []struct {
		name     string
		policy   *ApplicationSyncPolicy
		expected bool
	}{
		{"no policy at all", nil, false},
		{"policy without an automated block", &ApplicationSyncPolicy{}, false},
		{"automated block", &ApplicationSyncPolicy{Automated: &ApplicationSyncPolicyAutomated{}}, true},
		{"automated block explicitly enabled", &ApplicationSyncPolicy{Automated: &ApplicationSyncPolicyAutomated{Enabled: &enabled}}, true},
		{"automated block explicitly disabled", &ApplicationSyncPolicy{Automated: &ApplicationSyncPolicyAutomated{Enabled: &disabled}}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := Application{}
			application.Spec.SyncPolicy = test.policy
			assert.Equal(t, test.expected, application.AutoSyncEnabled())
		})
	}
}

func TestListUnhealthyResources(t *testing.T) {
	jsonData, err := os.ReadFile("testdata/failed-deployment.json")
	if err != nil {
		t.Fatalf("Failed to read JSON data file: %s", err)
	}

	var app Application
	err = json.Unmarshal(jsonData, &app)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %s", err)
	}

	unhealthyResources := app.ListUnhealthyResources()

	expectedResources := []string{
		"Pod(app-pod) Synced",
		"Job(app-job) Unhealthy with message Job has reached the specified backoff limit",
	}

	assert.Equal(t, expectedResources, unhealthyResources)
}

func TestNotSyncedDiagnosticsFromPayload(t *testing.T) {
	// Decodes a captured ArgoCD payload so the struct tags the not-synced report depends on —
	// status.resources[].status, status.resources[].requiresPruning, status.conditions, both
	// revisions and spec.syncPolicy — are exercised. Tests that build the Application in Go
	// cannot catch a wrong tag, which would leave the sections silently empty.
	jsonData, err := os.ReadFile("testdata/out-of-sync-deployment.json")
	if err != nil {
		t.Fatalf("Failed to read JSON data file: %s", err)
	}

	var app Application
	if err := json.Unmarshal(jsonData, &app); err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %s", err)
	}

	assert.Equal(t, ArgoRolloutAppNotSynced,
		app.GetRolloutStatus([]string{"product-catalog:v0.0.2"}, "", false))

	assert.Equal(t,
		"The last sync applied revision b6a9f1c, but ArgoCD now compares the application against "+
			"revision d4c2b0a, so the desired state has changed since that sync.\n"+
			"Auto-sync is enabled (prune on, self-heal off).\n\n"+
			"Sync errors:\n"+
			"\tComparisonError: Failed to load target state: rpc error: code = Unknown\n\n"+
			"Out-of-sync resources:\n"+
			"\tDeployment(product-catalog) OutOfSync\n"+
			"\tConfigMap(product-catalog-legacy) OutOfSync (requires pruning)\n\n"+
			"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
		app.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"product-catalog:v0.0.2"}, nil))
}

func TestNotSyncedDiagnosticsFromMultiSourcePayload(t *testing.T) {
	// The sibling payload test covers the single-source tags. This one covers the three a
	// multi-source application depends on — status.sync.revisions, syncResult.revisions and
	// spec.syncPolicy.automated.enabled — because a wrong tag on any of them fails silently: the
	// drift explanation would vanish, or an app with automated sync switched off would be reported
	// as one ArgoCD is about to fix by itself.
	jsonData, err := os.ReadFile("testdata/out-of-sync-multi-source.json")
	if err != nil {
		t.Fatalf("Failed to read JSON data file: %s", err)
	}

	var app Application
	if err := json.Unmarshal(jsonData, &app); err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %s", err)
	}

	assert.Equal(t, ArgoRolloutAppNotSynced,
		app.GetRolloutStatus([]string{"product-catalog:v0.0.2"}, "", false))

	assert.Equal(t,
		"The last sync applied revisions b6a9f1c, 1.14.2, but ArgoCD now compares the application "+
			"against revisions b6a9f1c, 1.15.0, so the desired state has changed since that sync.\n"+
			"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
			"Out-of-sync resources:\n"+
			"\tDeployment(product-catalog) OutOfSync\n\n"+
			"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
		app.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"product-catalog:v0.0.2"}, nil))
}

func TestArgoRolloutStatus(t *testing.T) {
	t.Run("Rollout status - ArgoRolloutAppNotAvailable", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		images := []string{"ghcr.io/shini4i/argo-watcher:version2"}
		registryProxyUrl := ""
		assert.Equal(t, ArgoRolloutAppNotAvailable, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppNotSynced", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Syncing"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		assert.Equal(t, ArgoRolloutAppNotSynced, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppNotHealthy", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "NotHealthy"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		assert.Equal(t, ArgoRolloutAppNotHealthy, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppSuccess", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Healthy"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		assert.Equal(t, ArgoRolloutAppSuccess, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - ArgoRolloutAppDegraded", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Degraded"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		registryProxyUrl := ""
		assert.Equal(t, ArgoRolloutAppDegraded, application.GetRolloutStatus(images, registryProxyUrl, false))
	})

	t.Run("Rollout status - a degraded app that is still OutOfSync is not synced", func(t *testing.T) {
		// A pending sync may yet recover the app, so drift outranks degradation here. The
		// not-synced failure report relies on this routing to know it must carry the pod cause.
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "OutOfSync"
		application.Status.Health.Status = "Degraded"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		assert.Equal(t, ArgoRolloutAppNotSynced, application.GetRolloutStatus(images, "", false))
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
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		images := []string{"ghcr.io/shini4i/argo-watcher:version2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tghcr.io/shini4i/argo-watcher:version1\n\nList of expected images:\n\tghcr.io/shini4i/argo-watcher:version2",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil))
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
				"Failed resources:\n"+
				"\tJob(app-migrations) PreSync Failed with message Job has reached the specified backoff limit",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil))
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
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil))
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
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil))
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
				"Failed resources:\n"+
				"\tJob(app-migrations) PreSync Failed with message Job has reached the specified backoff limit\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-pod) Degraded with message Back-off restarting failed container",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil))
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
				application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil),
				"phase=%q must produce the baseline image-only message", phase)
		}
		runCase("Running")
		runCase("Succeeded")
	})

	t.Run("Rollout message - ArgoRolloutAppNotSynced", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Syncing"
		application.Status.Health.Status = "Healthy"
		application.Status.OperationState.Phase = "NotWorking"
		application.Status.OperationState.Message = "Not working test app"
		application.Status.OperationState.SyncResult.Resources = make([]ApplicationOperationResource, 2)
		application.Status.OperationState.SyncResult.Resources[0].HookPhase = "Succeeded"
		application.Status.OperationState.SyncResult.Resources[0].HookType = "PreSync"
		application.Status.OperationState.SyncResult.Resources[0].Kind = "Pod"
		application.Status.OperationState.SyncResult.Resources[0].Message = ""
		application.Status.OperationState.SyncResult.Resources[0].Status = "Synced"
		application.Status.OperationState.SyncResult.Resources[0].SyncPhase = "PreSync"
		application.Status.OperationState.SyncResult.Resources[0].Name = "app-migrations"
		application.Status.OperationState.SyncResult.Resources[0].Namespace = "app"
		application.Status.OperationState.SyncResult.Resources[1].HookPhase = "Failed"
		application.Status.OperationState.SyncResult.Resources[1].HookType = "PostSync"
		application.Status.OperationState.SyncResult.Resources[1].Kind = "Job"
		application.Status.OperationState.SyncResult.Resources[1].Message = "Job has reached the specified backoff limit"
		application.Status.OperationState.SyncResult.Resources[1].Status = "Synced"
		application.Status.OperationState.SyncResult.Resources[1].SyncPhase = "PostSync"
		application.Status.OperationState.SyncResult.Resources[1].Name = "app-job"
		application.Status.OperationState.SyncResult.Resources[1].Namespace = "app"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		assert.Equal(t,
			"Failed resources:\n"+
				"\tJob(app-job) PostSync Failed with message Job has reached the specified backoff limit\n\n"+
				"Last sync operation: NotWorking, message: \"Not working test app\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, images, nil))
	})

	t.Run("Rollout message - ArgoRolloutAppNotHealthy", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		application.Status.Sync.Status = "Syncing"
		application.Status.Health.Status = "NotHealthy"
		application.Status.OperationState.Phase = "NotWorking"
		application.Status.OperationState.Message = "Not working test app"
		application.Status.Resources = make([]ApplicationResource, 2)
		application.Status.Resources[0].Kind = "Pod"
		application.Status.Resources[0].Name = "app-pod"
		application.Status.Resources[0].Namespace = "app"
		application.Status.Resources[0].Health.Message = ""
		application.Status.Resources[0].Health.Status = "Synced"
		application.Status.Resources[1].Kind = "Job"
		application.Status.Resources[1].Name = "app-job"
		application.Status.Resources[1].Namespace = "app"
		application.Status.Resources[1].Health.Message = "Job has reached the specified backoff limit"
		application.Status.Resources[1].Health.Status = "Unhealthy"
		images := []string{"ghcr.io/shini4i/argo-watcher:version1"}
		assert.Equal(t,
			"App sync status \"Syncing\"\nApp health status \"NotHealthy\"\nResources:\n\tPod(app-pod) Synced\n\tJob(app-job) Unhealthy with message Job has reached the specified backoff limit",
			application.GetRolloutMessage(ArgoRolloutAppNotHealthy, images, nil))
	})

	t.Run("Rollout message - default", func(t *testing.T) {
		application := Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:version1"}
		images := []string{"ghcr.io/shini4i/argo-watcher:version2"}
		assert.Equal(t, "received unexpected rollout status \"unexpected status\"", application.GetRolloutMessage("unexpected status", images, nil))
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
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, nil))
	})

	t.Run("Rollout message - ArgoRolloutAppNotAvailable surfaces pod cause from resource tree", func(t *testing.T) {
		// The real not-available cause (ImagePullBackOff) lives on a Pod, which never appears in
		// Status.Resources. When the live tree is supplied, its problem nodes must surface it.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-xyz", "Degraded", `Back-off pulling image "app:v0.0.2": ErrImagePull`),
			// Progressing parent must be filtered out so it does not dilute the signal.
			treeNode("Deployment", "app", "Progressing", "Waiting for rollout to finish"),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-xyz) Degraded with message Back-off pulling image \"app:v0.0.2\": ErrImagePull",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, tree))
	})

	t.Run("Rollout message - resource tree is preferred over Status.Resources", func(t *testing.T) {
		// When both are present the live tree wins: it carries the pods, so it is strictly more
		// informative than the top-level Status.Resources fallback.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.Resources = []ApplicationResource{
			{
				Kind: "Deployment", Name: "app", Namespace: "app",
				Health: struct {
					Message string `json:"message"`
					Status  string `json:"status"`
				}{Status: "Degraded", Message: "stale top-level status"},
			},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-xyz", "Degraded", "CrashLoopBackOff: back-off restarting failed container"),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-xyz) Degraded with message CrashLoopBackOff: back-off restarting failed container",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, tree))
	})

	t.Run("Rollout message - ArgoRolloutAppNotHealthy appends pod cause from resource tree", func(t *testing.T) {
		// A stalled rollout reported as not-healthy must also surface the pod-level cause, which the
		// pre-existing "Resources:" listing (top-level resources only) cannot show.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Progressing"
		application.Status.Resources = []ApplicationResource{
			{
				Kind: "Deployment", Name: "app", Namespace: "app",
				Health: struct {
					Message string `json:"message"`
					Status  string `json:"status"`
				}{Status: "Progressing", Message: "Waiting for rollout to finish"},
			},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-xyz", "Degraded", `Back-off pulling image "app:v0.0.2": ErrImagePull`),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"App sync status \"Synced\"\n"+
				"App health status \"Progressing\"\n"+
				"Resources:\n"+
				"\tDeployment(app) Progressing with message Waiting for rollout to finish\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-xyz) Degraded with message Back-off pulling image \"app:v0.0.2\": ErrImagePull",
			application.GetRolloutMessage(ArgoRolloutAppNotHealthy, images, tree))
	})

	t.Run("Rollout message - not available combines sync, hooks, and tree pod cause", func(t *testing.T) {
		// All three diagnostic sections must fire together when a live tree is present: a non-nil
		// tree must not short-circuit the terminal-sync and failed-hooks sections.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.OperationState.Phase = "Failed"
		application.Status.OperationState.Message = "one or more synchronization tasks completed unsuccessfully"
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{HookPhase: "Failed", HookType: "PreSync", Kind: "Job", Message: "Job has reached the specified backoff limit", SyncPhase: "PreSync", Name: "app-migrations", Namespace: "app"},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-xyz", "Degraded", `Back-off pulling image "app:v0.0.2": ErrImagePull`),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Sync operation phase: Failed\n"+
				"Sync operation message: one or more synchronization tasks completed unsuccessfully\n\n"+
				"Failed resources:\n"+
				"\tJob(app-migrations) PreSync Failed with message Job has reached the specified backoff limit\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-xyz) Degraded with message Back-off pulling image \"app:v0.0.2\": ErrImagePull",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, tree))
	})

	t.Run("Rollout message - not healthy with nil tree keeps sync+hooks and does not duplicate resources", func(t *testing.T) {
		// With no live tree, the not-healthy path must still surface the terminal-sync and
		// failed-hooks sections, but must NOT re-list the Degraded resource under "Unhealthy
		// resources:" — it already appears in the base "Resources:" block.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Progressing"
		application.Status.Resources = []ApplicationResource{
			{
				Kind: "Deployment", Name: "app", Namespace: "app",
				Health: struct {
					Message string `json:"message"`
					Status  string `json:"status"`
				}{Status: "Degraded", Message: "replicas unavailable"},
			},
		}
		application.Status.OperationState.Phase = "Failed"
		application.Status.OperationState.Message = "sync failed"
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{HookPhase: "Failed", HookType: "PreSync", Kind: "Job", Message: "backoff", SyncPhase: "PreSync", Name: "app-migrations", Namespace: "app"},
		}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"App sync status \"Synced\"\n"+
				"App health status \"Progressing\"\n"+
				"Resources:\n"+
				"\tDeployment(app) Degraded with message replicas unavailable\n\n"+
				"Sync operation phase: Failed\n"+
				"Sync operation message: sync failed\n\n"+
				"Failed resources:\n"+
				"\tJob(app-migrations) PreSync Failed with message backoff",
			application.GetRolloutMessage(ArgoRolloutAppNotHealthy, images, nil))
	})

	t.Run("Rollout message - ArgoRolloutAppDegraded reports the degraded resource and its pod cause", func(t *testing.T) {
		// A degraded rollout shares the not-healthy message shape and must not fall through to the
		// unexpected-status message. This is the terminal failure an operator reads most often — a
		// migration Job that exhausted its backoff limit — so the Job and the pod behind it must
		// both be named.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.2"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Degraded"
		application.Status.Resources = []ApplicationResource{
			{
				Kind: "Job", Name: "app-db-migrations", Namespace: "app",
				Health: struct {
					Message string `json:"message"`
					Status  string `json:"status"`
				}{Status: "Degraded", Message: "Job has reached the specified backoff limit"},
			},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-db-migrations-abcde", "Degraded", "back-off 5m0s restarting failed container=migrate"),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"App sync status \"Synced\"\n"+
				"App health status \"Degraded\"\n"+
				"Resources:\n"+
				"\tJob(app-db-migrations) Degraded with message Job has reached the specified backoff limit\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(app-db-migrations-abcde) Degraded with message back-off 5m0s restarting failed container=migrate",
			application.GetRolloutMessage(ArgoRolloutAppDegraded, images, tree))
	})

	t.Run("Rollout message - not healthy omits the Resources block when no resource reports health", func(t *testing.T) {
		// ArgoCD reports no health for kinds it cannot assess, so the unhealthy-resource list can
		// come back empty. The block must then be absent entirely rather than printed as a bare
		// "Resources:" header with nothing under it, which reads as "collection failed".
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.2"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Progressing"
		application.Status.Resources = []ApplicationResource{
			{Kind: "ServiceAccount", Name: "app", Namespace: "app"},
		}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"App sync status \"Synced\"\n"+
				"App health status \"Progressing\"",
			application.GetRolloutMessage(ArgoRolloutAppNotHealthy, images, nil))
	})

	t.Run("Rollout message - not synced closes with the last sync operation", func(t *testing.T) {
		// Every other section can empty out, so the operation line is what keeps the report from
		// being blank. It closes the report deliberately: leading with a succeeded operation is
		// what made the failure read as self-contradictory.
		application := Application{}
		application.Status.OperationState.Phase = "NotWorking"
		application.Status.OperationState.Message = "Not working test app"
		assert.Equal(t,
			"Last sync operation: NotWorking, message: \"Not working test app\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced says so when no sync operation is recorded", func(t *testing.T) {
		application := Application{}
		assert.Equal(t,
			"No sync operation is recorded for this application.",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced explains a superseded revision", func(t *testing.T) {
		// The reported confusion: the sync operation succeeded while the application stayed out of
		// sync, which reads as a contradiction until the report says the target moved underneath it.
		//
		// The wording claims no ordering between the two revisions on purpose — a rollback, a revert
		// or a retargeted branch all leave the comparison pointing at an older revision, so calling
		// it "newer" would send the operator looking for a commit that does not exist.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)",
			"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "0f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f10")
		assert.Equal(t,
			"The last sync applied revision a1b2c3d, but ArgoCD now compares the application against "+
				"revision 0f9e8d7, so the desired state has changed since that sync.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced compares the untruncated revisions", func(t *testing.T) {
		// Two distinct commits can share an abbreviated prefix. Comparing the abbreviations would
		// report a moved target as drift that "will not converge" — the opposite verdict.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)",
			"a1b2c3d0000000000000000000000000000000ff", "a1b2c3d1111111111111111111111111111111ff")
		assert.Equal(t,
			"The last sync applied revision a1b2c3d, but ArgoCD now compares the application against "+
				"revision a1b2c3d, so the desired state has changed since that sync.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced calls out drift that will never converge", func(t *testing.T) {
		// The same revision on both sides means the live state diverges right after being applied,
		// so no amount of extra waiting helps. Saying that is the whole point of the section.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)",
			"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0")
		application.Spec.SyncPolicy = &ApplicationSyncPolicy{Automated: &ApplicationSyncPolicyAutomated{SelfHeal: true}}
		assert.Equal(t,
			"The last sync succeeded for revision a1b2c3d and the application is still out of sync "+
				"against exactly what that sync applied, so applying it did not converge the live "+
				"state. Usual causes: a mutating admission webhook, a controller that owns a field "+
				"the manifests also set (replicas versus an HPA), a resource that has to be pruned, "+
				"or a sync that covered only some resources. Extra waiting is unlikely to help.\n"+
				"Auto-sync is enabled (prune off, self-heal on).\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced reads correctly for a multi-source application", func(t *testing.T) {
		// The plural phrase has to sit in a sentence that does not then refer to "that revision".
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Sync.Revisions = []string{"aaa1111", "bbb2222"}
		application.Status.OperationState.SyncResult.Revisions = []string{"aaa1111", "bbb2222"}
		assert.Equal(t,
			"The last sync succeeded for revisions aaa1111, bbb2222 and the application is still out "+
				"of sync against exactly what that sync applied, so applying it did not converge the "+
				"live state. Usual causes: a mutating admission webhook, a controller that owns a "+
				"field the manifests also set (replicas versus an HPA), a resource that has to be "+
				"pruned, or a sync that covered only some resources. Extra waiting is unlikely to "+
				"help.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced draws no revision verdict when the sync itself failed", func(t *testing.T) {
		// The verdict compares a *successful* apply against the current comparison. After a failed
		// sync, matching revisions mean nothing was applied, not that the live state drifts back.
		application := notSyncedApp("Failed", "one or more synchronization tasks completed unsuccessfully",
			"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0")
		assert.Equal(t,
			"Last sync operation: Failed, message: \"one or more synchronization tasks completed unsuccessfully\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced compares the revisions of a multi-source application", func(t *testing.T) {
		// Multi-source applications report revisions[] and leave revision empty; comparing the two
		// empty strings would label every such failure as permanent drift.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Sync.Revisions = []string{"aaa1111", "bbb2222"}
		application.Status.OperationState.SyncResult.Revisions = []string{"aaa1111", "ccc3333"}
		assert.Equal(t,
			"The last sync applied revisions aaa1111, ccc3333, but ArgoCD now compares the application "+
				"against revisions aaa1111, bbb2222, so the desired state has changed since that sync.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced drops the resources that applied cleanly", func(t *testing.T) {
		// The reported case: every resource applied ("configured"), yet the report listed them as
		// "Running" — gitops-engine's phase for a successful apply — which read as a stalled sync.
		// Only the entry that actually failed belongs in a failure report.
		application := notSyncedApp("Failed", "one or more synchronization tasks completed unsuccessfully", "", "")
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{
				Kind:      "Deployment",
				Name:      "product-catalog",
				Namespace: "app",
				Status:    "Synced",
				HookPhase: "Running",
				SyncPhase: "Sync",
				Message:   "deployment.apps/product-catalog configured",
			},
			{
				Kind:      "ConfigMap",
				Name:      "product-catalog",
				Namespace: "app",
				Status:    "SyncFailed",
				HookPhase: "Failed",
				SyncPhase: "Sync",
				Message:   "error validating data: unknown field \"dat\"",
			},
		}
		application.Status.Resources = []ApplicationResource{
			{Kind: "ConfigMap", Name: "product-catalog", Namespace: "app", Status: "OutOfSync"},
		}
		assert.Equal(t,
			"Out-of-sync resources:\n"+
				"\tConfigMap(product-catalog) OutOfSync\n\n"+
				"Failed resources:\n"+
				"\tConfigMap(product-catalog) SyncFailed with message error validating data: unknown field \"dat\"\n\n"+
				"Last sync operation: Failed, message: \"one or more synchronization tasks completed unsuccessfully\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced reports the pod-level cause when the app is also degraded", func(t *testing.T) {
		// A Degraded application that is still OutOfSync is classified "not synced", so this arm is
		// the only place the crashlooping pod behind such a failure is ever reported. The cause lives
		// in the resource tree alone — Status.Resources carries no pod health.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Resources = []ApplicationResource{
			{Kind: "Deployment", Name: "product-catalog", Namespace: "app", Status: "OutOfSync"},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "product-catalog-abcde", "Degraded", "back-off 5m0s restarting failed container=app"),
			treeNode("Service", "product-catalog", "Healthy", ""),
		}}
		assert.Equal(t,
			"Unhealthy resources:\n"+
				"\tPod(product-catalog-abcde) Degraded with message back-off 5m0s restarting failed container=app\n\n"+
				"Out-of-sync resources:\n"+
				"\tDeployment(product-catalog) OutOfSync\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, tree))
	})

	t.Run("Rollout message - not synced adds no Unhealthy block when the tree is clean", func(t *testing.T) {
		// The tree is fetched on every failure, so a healthy one must leave the report untouched.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Resources = []ApplicationResource{
			{Kind: "Deployment", Name: "product-catalog", Namespace: "app", Status: "OutOfSync"},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "product-catalog-abcde", "Healthy", ""),
		}}
		assert.Equal(t,
			"Out-of-sync resources:\n"+
				"\tDeployment(product-catalog) OutOfSync\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, tree))
	})

	t.Run("Rollout message - not synced omits the Sync errors block when only warnings are present", func(t *testing.T) {
		// The conditions filter emptying out must leave no trace: no header, no separator. Warnings
		// are the common case on a healthy app, so this is the path most likely to regress.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Conditions = []ApplicationCondition{
			{Type: "OrphanedResourceWarning", Message: "Application has 1 orphaned resource"},
			{Type: "SharedResourceWarning", Message: "Resource is part of applications app-a and app-b"},
		}
		assert.Equal(t,
			"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced omits the Out-of-sync block when no resource has drifted", func(t *testing.T) {
		// The app-level sync status can be non-Synced while every top-level resource compares clean
		// (or is not compared at all), so the resource filter must be able to empty out silently.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Resources = []ApplicationResource{
			{Kind: "Service", Name: "product-catalog", Namespace: "app", Status: "Synced"},
			{Kind: "Endpoints", Name: "product-catalog", Namespace: "app"},
		}
		assert.Equal(t,
			"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced names the resources that are out of sync", func(t *testing.T) {
		// A resource that only exists in the cluster is annotated: it reaches Synced by being
		// deleted, which a sync without prune never does, so no amount of waiting fixes it.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Resources = []ApplicationResource{
			{Kind: "Deployment", Name: "product-catalog", Namespace: "app", Status: "OutOfSync"},
			{Kind: "Service", Name: "product-catalog", Namespace: "app", Status: "Synced"},
			{Kind: "ConfigMap", Name: "product-catalog", Namespace: "app", Status: "Unknown"},
			{Kind: "Secret", Name: "product-catalog-old", Namespace: "app", Status: "OutOfSync", RequiresPruning: true},
			// A resource ArgoCD does not compare carries no status: absent means "not assessed",
			// not "drifted", so it must not be reported as a culprit.
			{Kind: "Endpoints", Name: "product-catalog", Namespace: "app"},
		}
		assert.Equal(t,
			"Out-of-sync resources:\n"+
				"\tDeployment(product-catalog) OutOfSync\n"+
				"\tConfigMap(product-catalog) Unknown\n"+
				"\tSecret(product-catalog-old) OutOfSync (requires pruning)\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced surfaces error conditions and ignores warnings", func(t *testing.T) {
		// A sync status of "Unknown" means ArgoCD could not compare at all, and the reason lives
		// only in the conditions. Warnings are advisory and would dilute the signal.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Conditions = []ApplicationCondition{
			{Type: "ComparisonError", Message: "Failed to load target state: rpc error: code = Unknown"},
			{Type: "OrphanedResourceWarning", Message: "Application has 1 orphaned resource"},
			{Type: "InvalidSpecError", Message: "Application referencing project default which does not exist"},
		}
		assert.Equal(t,
			"Sync errors:\n"+
				"\tComparisonError: Failed to load target state: rpc error: code = Unknown\n"+
				"\tInvalidSpecError: Application referencing project default which does not exist\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced reports drift and errors together", func(t *testing.T) {
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.Resources = []ApplicationResource{
			{Kind: "Deployment", Name: "product-catalog", Namespace: "app", Status: "OutOfSync"},
		}
		application.Status.Conditions = []ApplicationCondition{
			{Type: "SyncError", Message: "Failed sync attempt to v1.2.3"},
		}
		assert.Equal(t,
			"Sync errors:\n"+
				"\tSyncError: Failed sync attempt to v1.2.3\n\n"+
				"Out-of-sync resources:\n"+
				"\tDeployment(product-catalog) OutOfSync\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced names an operation whose phase is missing", func(t *testing.T) {
		// The operation line is the one section that always renders, so both of its degenerate
		// shapes are exactly what a user sees when ArgoCD's payload is sparse.
		application := Application{}
		application.Status.OperationState.Message = "Not working test app"
		assert.Equal(t,
			"Last sync operation: unknown, message: \"Not working test app\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced leaves no dangling comma without an operation message", func(t *testing.T) {
		application := Application{}
		application.Status.OperationState.Phase = "Running"
		assert.Equal(t,
			"Last sync operation: Running",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced draws no verdict when only one revision is known", func(t *testing.T) {
		// Half the comparison is no comparison: rendering it would end the sentence at "against ."
		// and the auto-sync note that rides along would be lost with it.
		for _, test := range []struct{ name, applied, compared string }{
			{"only the applied revision", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", ""},
			{"only the compared revision", "", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"},
		} {
			t.Run(test.name, func(t *testing.T) {
				application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", test.applied, test.compared)
				assert.Equal(t,
					"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
					application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
			})
		}
	})

	t.Run("Rollout message - not synced keeps the phrase singular for a one-source revisions list", func(t *testing.T) {
		// A single-source application whose payload uses revisions[] must not read "revisions abc".
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.OperationState.SyncResult.Revisions = []string{"aaa1111"}
		application.Status.Sync.Revisions = []string{"bbb2222"}
		assert.Equal(t,
			"The last sync applied revision aaa1111, but ArgoCD now compares the application against "+
				"revision bbb2222, so the desired state has changed since that sync.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced prefers the revisions list when both fields are populated", func(t *testing.T) {
		// Deciding on the singular field would compare the first source only, so a second source
		// that moved would be reported as drift that never converges — the opposite verdict.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "aaa1111", "aaa1111")
		application.Status.OperationState.SyncResult.Revisions = []string{"aaa1111", "ccc3333"}
		application.Status.Sync.Revisions = []string{"aaa1111", "bbb2222"}
		assert.Equal(t,
			"The last sync applied revisions aaa1111, ccc3333, but ArgoCD now compares the application "+
				"against revisions aaa1111, bbb2222, so the desired state has changed since that sync.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced trusts the singular field over a one-entry list", func(t *testing.T) {
		// A single-source application reports its revision in the singular field, so when a payload
		// carries both the list adds no source to compare and the singular field decides. Only a
		// list of more than one entry outranks it.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "aaa1111", "aaa1111")
		application.Status.OperationState.SyncResult.Revisions = []string{"bbb2222"}
		application.Status.Sync.Revisions = []string{"ccc3333"}
		assert.Equal(t,
			"The last sync succeeded for revision aaa1111 and the application is still out of sync "+
				"against exactly what that sync applied, so applying it did not converge the live "+
				"state. Usual causes: a mutating admission webhook, a controller that owns a field "+
				"the manifests also set (replicas versus an HPA), a resource that has to be pruned, "+
				"or a sync that covered only some resources. Extra waiting is unlikely to help.\n"+
				"Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered.\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced leaves a skipped prune out of the failed resources", func(t *testing.T) {
		// A prune ArgoCD declined to perform is not a failure, and the out-of-sync listing already
		// annotates it with "(requires pruning)" — reporting it twice would only dilute the signal.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)", "", "")
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{
				Kind:      "Secret",
				Name:      "product-catalog-old",
				Namespace: "app",
				Status:    "PruneSkipped",
				HookPhase: "Succeeded",
				SyncPhase: "Sync",
			},
		}
		application.Status.Resources = []ApplicationResource{
			{Kind: "Secret", Name: "product-catalog-old", Namespace: "app", Status: "OutOfSync", RequiresPruning: true},
		}
		assert.Equal(t,
			"Out-of-sync resources:\n"+
				"\tSecret(product-catalog-old) OutOfSync (requires pruning)\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, nil))
	})

	t.Run("Rollout message - not synced orders every section of a full report", func(t *testing.T) {
		// The section order is the substance of this report: the drift explanation answers the
		// question first, and the sync operation — which routinely succeeded — closes it as context
		// instead of opening it as an apparent contradiction. One assertion pins the whole shape.
		application := notSyncedApp("Succeeded", "successfully synced (all tasks run)",
			"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "0f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f10")
		application.Spec.SyncPolicy = &ApplicationSyncPolicy{Automated: &ApplicationSyncPolicyAutomated{Prune: true}}
		application.Status.Conditions = []ApplicationCondition{
			{Type: "SyncError", Message: "Failed sync attempt to v1.2.3"},
		}
		application.Status.Resources = []ApplicationResource{
			{Kind: "Deployment", Name: "order-management", Namespace: "app", Status: "OutOfSync"},
			{Kind: "Secret", Name: "order-management-old", Namespace: "app", Status: "OutOfSync", RequiresPruning: true},
		}
		application.Status.OperationState.SyncResult.Resources = []ApplicationOperationResource{
			{Kind: "ConfigMap", Name: "order-management", Namespace: "app", Status: "SyncFailed", HookPhase: "Failed", Message: "error validating data"},
		}
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "order-management-abcde", "Degraded", "back-off 5m0s restarting failed container=app"),
		}}
		assert.Equal(t,
			"The last sync applied revision a1b2c3d, but ArgoCD now compares the application against "+
				"revision 0f9e8d7, so the desired state has changed since that sync.\n"+
				"Auto-sync is enabled (prune on, self-heal off).\n\n"+
				"Sync errors:\n"+
				"\tSyncError: Failed sync attempt to v1.2.3\n\n"+
				"Unhealthy resources:\n"+
				"\tPod(order-management-abcde) Degraded with message back-off 5m0s restarting failed container=app\n\n"+
				"Out-of-sync resources:\n"+
				"\tDeployment(order-management) OutOfSync\n"+
				"\tSecret(order-management-old) OutOfSync (requires pruning)\n\n"+
				"Failed resources:\n"+
				"\tConfigMap(order-management) SyncFailed with message error validating data\n\n"+
				"Last sync operation: Succeeded, message: \"successfully synced (all tasks run)\"",
			application.GetRolloutMessage(ArgoRolloutAppNotSynced, []string{"app:v0.0.2"}, tree))
	})

	t.Run("Rollout message - not healthy reports what is still progressing when nothing is degraded", func(t *testing.T) {
		// A rollout that simply ran out its timeout has nothing Degraded: every node is still
		// Progressing, which isProblemHealthStatus deliberately excludes. Without this fallback the
		// operator is told only "Progressing" with no indication of what never became ready.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.2"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Progressing"
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Deployment", "app", "Progressing", "Waiting for rollout to finish: 0 of 1 updated replicas are available..."),
			treeNode("Pod", "app-xyz", "Progressing", "container has not become ready"),
			treeNode("Service", "app", "Healthy", ""),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"App sync status \"Synced\"\n"+
				"App health status \"Progressing\"\n\n"+
				"Resources still progressing:\n"+
				"\tDeployment(app) Progressing with message Waiting for rollout to finish: 0 of 1 updated replicas are available...\n"+
				"\tPod(app-xyz) Progressing with message container has not become ready",
			application.GetRolloutMessage(ArgoRolloutAppNotHealthy, images, tree))
	})

	t.Run("Rollout message - a degraded app suppresses the still-progressing fallback", func(t *testing.T) {
		// Suppression keys on the app's own health, not on the degraded node: once the application
		// itself is Degraded that IS the culprit, and listing the Progressing siblings alongside it
		// would dilute the signal the "Unhealthy resources:" section exists to carry.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.2"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Degraded"
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-xyz", "Degraded", "CrashLoopBackOff"),
			treeNode("Deployment", "app", "Progressing", "Waiting for rollout to finish"),
		}}
		images := []string{"app:v0.0.2"}
		msg := application.GetRolloutMessage(ArgoRolloutAppDegraded, images, tree)
		assert.Contains(t, msg, "Unhealthy resources:\n\tPod(app-xyz) Degraded with message CrashLoopBackOff")
		assert.NotContains(t, msg, "Resources still progressing:")
	})

	t.Run("Rollout message - a problem resource that is not the culprit still reports what is progressing", func(t *testing.T) {
		// isProblemHealthStatus also matches Suspended/Missing/Unknown. A suspended CronJob is a
		// problem node but not why the rollout stalled, so it must not hide the workload that never
		// became ready: both sections are reported when the app itself is still Progressing.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.2"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Progressing"
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("CronJob", "backup", "Suspended", ""),
			treeNode("Deployment", "app", "Progressing", "Waiting for rollout to finish"),
		}}
		msg := application.GetRolloutMessage(ArgoRolloutAppNotHealthy, []string{"app:v0.0.2"}, tree)
		assert.Contains(t, msg, "Unhealthy resources:\n\tCronJob(backup) Suspended")
		assert.Contains(t, msg, "Resources still progressing:\n\tDeployment(app) Progressing with message Waiting for rollout to finish")
	})

	t.Run("Rollout message - not available reports what is still progressing when nothing is degraded", func(t *testing.T) {
		// The same fallback must serve the not-available arm: an image that never appears because
		// its pod is stuck Pending has no Degraded node either.
		application := Application{}
		application.Status.Summary.Images = []string{"app:v0.0.1"}
		application.Status.Health.Status = "Progressing"
		tree := &ApplicationTree{Nodes: []ApplicationTreeNode{
			treeNode("Pod", "app-xyz", "Progressing", "Pod is in Pending state"),
		}}
		images := []string{"app:v0.0.2"}
		assert.Equal(t,
			"List of current images (last app check):\n\tapp:v0.0.1\n\n"+
				"List of expected images:\n\tapp:v0.0.2\n\n"+
				"Resources still progressing:\n"+
				"\tPod(app-xyz) Progressing with message Pod is in Pending state",
			application.GetRolloutMessage(ArgoRolloutAppNotAvailable, images, tree))
	})
}

// notSyncedApp builds the application shape a not-synced report starts from: the outcome of the
// last sync operation, the revision that sync applied, and the revision ArgoCD now compares
// against. Empty revisions stand for a payload where ArgoCD reported neither.
func notSyncedApp(phase, message, applied, compared string) Application {
	application := Application{}
	application.Status.Sync.Status = "OutOfSync"
	application.Status.Sync.Revision = compared
	application.Status.OperationState.Phase = phase
	application.Status.OperationState.Message = message
	application.Status.OperationState.SyncResult.Revision = applied
	return application
}

// treeNode spares call sites the anonymous struct literal for the Health field.
func treeNode(kind, name, status, message string) ApplicationTreeNode {
	n := ApplicationTreeNode{Kind: kind, Name: name, Namespace: "app"}
	n.Health.Status = status
	n.Health.Message = message
	return n
}

func TestApplicationTreeListProblemNodes(t *testing.T) {
	tree := ApplicationTree{Nodes: []ApplicationTreeNode{
		treeNode("Pod", "pull-fail", "Degraded", `Back-off pulling image "app:v2": ErrImagePull`),
		treeNode("Pod", "healthy", "Healthy", ""),
		treeNode("Deployment", "app", "Progressing", "Waiting for rollout to finish"),
		treeNode("Job", "missing-hook", "Missing", ""),
		treeNode("Pod", "unknown-pod", "Unknown", "lost contact with node"),
		treeNode("Rollout", "suspended-rollout", "Suspended", "paused for manual promotion"),
		treeNode("Service", "no-health", "", ""),
	}}

	got := tree.ListProblemNodes()

	assert.Equal(t, []string{
		`Pod(pull-fail) Degraded with message Back-off pulling image "app:v2": ErrImagePull`,
		"Job(missing-hook) Missing",
		"Pod(unknown-pod) Unknown with message lost contact with node",
		"Rollout(suspended-rollout) Suspended with message paused for manual promotion",
	}, got)
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

func TestIsImageValidationSkipped(t *testing.T) {
	tt := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{"opted out", map[string]string{skipImageValidationAnnotation: "true"}, true},
		{"explicitly enabled", map[string]string{skipImageValidationAnnotation: "false"}, false},
		{"other annotations only", map[string]string{managedAnnotation: "true"}, false},
		{"annotations are nil", nil, false},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			app := Application{Metadata: ApplicationMetadata{Annotations: tc.annotations}}
			assert.Equal(t, tc.want, app.IsImageValidationSkipped())
		})
	}
}
