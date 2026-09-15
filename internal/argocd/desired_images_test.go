package argocd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

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
	resources := models.ApplicationManifests{Manifests: []string{
		deploymentManifest,
		cronJobManifest,
		serviceManifest,
		deploymentManifest,
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
	resources := models.ApplicationManifests{Manifests: []string{
		"",
		"null",
		`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": "not-a-list"}}}}`,
		`{"kind": "Deployment", "spec": {"template": {"spec": {"containers": [{"name": "no-image"}]}}}}`,
		deploymentManifest,
	}}

	names, err := desiredImageNames(&resources)

	require.NoError(t, err)
	assert.Equal(t, []string{"busybox", "ghcr.io/shini4i/app"}, names)
}

// An undecodable manifest may be the one declaring the requested image, so the set is
// reported as unusable rather than as a smaller set of images.
func TestDesiredImageNamesReportsUndecodableItem(t *testing.T) {
	resources := models.ApplicationManifests{Manifests: []string{
		deploymentManifest,
		"not json",
	}}

	names, err := desiredImageNames(&resources)

	require.Error(t, err)
	assert.Nil(t, names)
	assert.Contains(t, err.Error(), "manifest 1")
	assert.NotNil(t, errors.Unwrap(err), "the decoder's error must stay in the chain")
}

// An image declared outside a pod template — an operator CR is the common case — still
// counts as part of the application.
func TestDesiredImageNamesCollectsNonTemplateImages(t *testing.T) {
	resources := models.ApplicationManifests{Manifests: []string{
		`{"kind":"Workflow","spec":{"templates":[{"container":{"image":"ghcr.io/shini4i/step:v1"}}]}}`,
		// An "image" key holding an object, not a reference, must not be recorded.
		`{"kind":"ConfigMap","data":{"image":{"repository":"ignored"}}}`,
	}}

	names, err := desiredImageNames(&resources)

	require.NoError(t, err)
	assert.Equal(t, []string{"ghcr.io/shini4i/step"}, names)
}

// A string under any key named "image" counts, wherever it sits. Rendered manifests feed in
// ConfigMaps and hook resources too, so stray matches are expected: they only widen the accepted
// set, and a non-empty set is what lets the caller conclude at all.
func TestDesiredImageNamesWidensOnAnyImageKey(t *testing.T) {
	resources := models.ApplicationManifests{Manifests: []string{
		`{"kind":"Deployment","spec":{"template":{"spec":{"containers":[{"image":"ghcr.io/shini4i/app:v1"}]}}}}`,
		`{"kind":"ConfigMap","data":{"image":"ghcr.io/shini4i/sidecar:v1"}}`,
	}}

	names, err := desiredImageNames(&resources)

	require.NoError(t, err)
	assert.Equal(t, []string{"ghcr.io/shini4i/app", "ghcr.io/shini4i/sidecar"}, names)
}

// "Cannot conclude" has two shapes, and callers respond to them differently: an empty
// list means the desired state declares no image, an error means it could not be read.
func TestDesiredImageNamesEmpty(t *testing.T) {
	resources := models.ApplicationManifests{Manifests: []string{serviceManifest}}
	names, err := desiredImageNames(&resources)
	require.NoError(t, err)
	assert.Empty(t, names)

	empty := models.ApplicationManifests{}
	names, err = desiredImageNames(&empty)
	require.NoError(t, err)
	assert.Empty(t, names)

	names, err = desiredImageNames(nil)
	require.NoError(t, err)
	assert.Nil(t, names)
}
