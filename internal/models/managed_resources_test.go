package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func resource(targetState string) ManagedResource {
	return ManagedResource{TargetState: targetState}
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
	resources := ManagedResources{Items: []ManagedResource{
		resource(deploymentManifest),
		resource(cronJobManifest),
		resource(serviceManifest),
		resource(deploymentManifest),
	}}

	assert.Equal(t, []string{
		"busybox",
		"ghcr.io/shini4i/app",
		"ghcr.io/shini4i/cleanup",
	}, resources.DesiredImageNames())
}

// A single unparsable manifest must not blind the check to the images it can read.
func TestDesiredImageNamesSkipsUnusableItems(t *testing.T) {
	resources := ManagedResources{Items: []ManagedResource{
		resource(""),
		resource("not json"),
		resource(`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": "not-a-list"}}}}`),
		resource(`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": [{"name": "no-image"}]}}}}`),
		resource(deploymentManifest),
	}}

	assert.Equal(t, []string{"busybox", "ghcr.io/shini4i/app"}, resources.DesiredImageNames())
}

// An image declared outside a pod template — an operator CR is the common case — still
// counts as part of the application.
func TestDesiredImageNamesCollectsNonTemplateImages(t *testing.T) {
	resources := ManagedResources{Items: []ManagedResource{
		resource(`{"kind":"Workflow","spec":{"templates":[{"container":{"image":"ghcr.io/shini4i/step:v1"}}]}}`),
		// An "image" key holding an object, not a reference, must not be recorded.
		resource(`{"kind":"ConfigMap","data":{"image":{"repository":"ignored"}}}`),
	}}

	assert.Equal(t, []string{"ghcr.io/shini4i/step"}, resources.DesiredImageNames())
}

// An empty list is what callers treat as "cannot conclude".
func TestDesiredImageNamesEmpty(t *testing.T) {
	resources := ManagedResources{Items: []ManagedResource{resource(serviceManifest)}}
	assert.Empty(t, resources.DesiredImageNames())

	empty := ManagedResources{}
	assert.Empty(t, empty.DesiredImageNames())
}
