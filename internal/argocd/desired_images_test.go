package argocd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	names, err := desiredImageNames(&resources)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"busybox",
		"ghcr.io/shini4i/app",
		"ghcr.io/shini4i/cleanup",
	}, names)
}

// A resource that declares no image contributes nothing, without making the whole set
// unusable: one that exists only in the cluster carries the target state "null" or none
// at all, and a container may simply have no image key.
func TestDesiredImageNamesSkipsImagelessItems(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{
		resource(""),
		resource("null"),
		resource(`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": "not-a-list"}}}}`),
		resource(`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": [{"name": "no-image"}]}}}}`),
		resource(deploymentManifest),
	}}

	names, err := desiredImageNames(&resources)

	require.NoError(t, err)
	assert.Equal(t, []string{"busybox", "ghcr.io/shini4i/app"}, names)
}

// An undecodable manifest may be the one declaring the requested image, so the set is
// reported as unusable rather than as a smaller set of images.
func TestDesiredImageNamesReportsUndecodableItem(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{
		resource(deploymentManifest),
		resource("not json"),
	}}

	names, err := desiredImageNames(&resources)

	require.Error(t, err)
	assert.Nil(t, names)
	assert.Contains(t, err.Error(), "item 1")
	assert.NotNil(t, errors.Unwrap(err), "the decoder's error must stay in the chain")
}

// An image declared outside a pod template — an operator CR is the common case — still
// counts as part of the application.
func TestDesiredImageNamesCollectsNonTemplateImages(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{
		resource(`{"kind":"Workflow","spec":{"templates":[{"container":{"image":"ghcr.io/shini4i/step:v1"}}]}}`),
		// An "image" key holding an object, not a reference, must not be recorded.
		resource(`{"kind":"ConfigMap","data":{"image":{"repository":"ignored"}}}`),
	}}

	names, err := desiredImageNames(&resources)

	require.NoError(t, err)
	assert.Equal(t, []string{"ghcr.io/shini4i/step"}, names)
}

// "Cannot conclude" has two shapes, and callers respond to them differently: an empty
// list means the desired state declares no image, an error means it could not be read.
func TestDesiredImageNamesEmpty(t *testing.T) {
	resources := models.ManagedResources{Items: []models.ManagedResource{resource(serviceManifest)}}
	names, err := desiredImageNames(&resources)
	require.NoError(t, err)
	assert.Empty(t, names)

	empty := models.ManagedResources{}
	names, err = desiredImageNames(&empty)
	require.NoError(t, err)
	assert.Empty(t, names)

	names, err = desiredImageNames(nil)
	require.NoError(t, err)
	assert.Nil(t, names)
}
