package argocd

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/internal/models"
)

func resource(targetState string) models.ManagedResource {
	return models.ManagedResource{TargetState: targetState}
}

const deploymentManifest = `{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "spec": {
    "template": {
      "spec": {
        "initContainers": [{"name": "wait", "image": "busybox:1.36"}],
        "containers": [{"name": "app", "image": "ghcr.io/shini4i/app:v1"}]
      }
    }
  }
}`

// cronJobManifest is the case argo-watcher must not get wrong: a CronJob that has
// never fired contributes no image to the application's live pod set.
const cronJobManifest = `{
  "apiVersion": "batch/v1",
  "kind": "CronJob",
  "spec": {
    "jobTemplate": {
      "spec": {
        "template": {
          "spec": {
            "containers": [{"name": "cleanup", "image": "ghcr.io/shini4i/cleanup:v1"}]
          }
        }
      }
    }
  }
}`

const serviceManifest = `{"apiVersion": "v1", "kind": "Service", "spec": {"ports": [{"port": 80}]}}`

func TestDesiredImageNamesCollectsWorkloadKinds(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{
		resource(deploymentManifest),
		resource(cronJobManifest),
		resource(serviceManifest),
		resource(deploymentManifest),
	}}

	assert.Equal(t, []string{
		"busybox",
		"ghcr.io/shini4i/app",
		"ghcr.io/shini4i/cleanup",
	}, desiredImageNames(&resources))
}

// A single unparsable manifest must not blind the check to the images it can read.
func TestDesiredImageNamesSkipsUnusableItems(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{
		resource(""),
		resource("not json"),
		resource(`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": "not-a-list"}}}}`),
		resource(`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": [{"name": "no-image"}]}}}}`),
		resource(deploymentManifest),
	}}

	assert.Equal(t, []string{"busybox", "ghcr.io/shini4i/app"}, desiredImageNames(&resources))
}

// An image declared outside a pod template — an operator CR is the common case — still
// counts as part of the application.
func TestDesiredImageNamesCollectsNonTemplateImages(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{
		resource(`{"kind":"Workflow","spec":{"templates":[{"container":{"image":"ghcr.io/shini4i/step:v1"}}]}}`),
		// An "image" key holding an object, not a reference, must not be recorded.
		resource(`{"kind":"ConfigMap","data":{"image":{"repository":"ignored"}}}`),
	}}

	assert.Equal(t, []string{"ghcr.io/shini4i/step"}, desiredImageNames(&resources))
}

// An empty list is what callers treat as "cannot conclude".
func TestDesiredImageNamesEmpty(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{resource(serviceManifest)}}
	assert.Empty(t, desiredImageNames(&resources))

	empty := models.ManagedResources{}
	assert.Empty(t, desiredImageNames(&empty))
}
