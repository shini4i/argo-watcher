package models

import (
	"encoding/json"
	"slices"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

// ManagedResources is the response of ArgoCD's
// /api/v1/applications/{name}/managed-resources endpoint: the desired state of every
// resource the application manages. It is the only reliable answer to "does this image
// belong to the application at all" — Application.Status.Summary.Images is derived
// exclusively from live Pods, so a workload with no running pod (an untriggered CronJob,
// a Deployment scaled to zero) is absent from it. ArgoCD filters hook resources out of
// this response, so images used only by sync hooks are absent here.
type ManagedResources struct {
	Items []ManagedResource `json:"items"`
}

// ManagedResource carries a resource's desired manifest, JSON-serialized as rendered
// from the application source. TargetState is empty for a resource that exists only
// in the cluster.
type ManagedResource struct {
	TargetState string `json:"targetState"`
}

// DesiredImageNames returns the sorted, de-duplicated repository names (tags and
// digests stripped) of every container image declared in the application's desired
// state. An item whose target state is empty or unparsable is skipped, so one bad
// manifest never hides the images the rest of them declare.
func (resources *ManagedResources) DesiredImageNames() []string {
	if resources == nil {
		return nil
	}

	found := make(map[string]struct{})

	for index := range resources.Items {
		targetState := resources.Items[index].TargetState
		if targetState == "" {
			continue
		}

		var manifest any
		if err := json.Unmarshal([]byte(targetState), &manifest); err != nil {
			continue
		}

		collectImages(manifest, found)
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

// collectImages records the repository name of every string stored under an "image"
// key, at any depth. Matching on the key alone rather than on known pod-template paths
// keeps Deployments, CronJobs, Rollouts and image-bearing CRDs on one code path.
// A stray match widens the accepted set, but it also makes the set non-empty, which is
// what lets the caller conclude at all — see DesiredImageNames' callers for how an empty
// set is treated.
func collectImages(node any, found map[string]struct{}) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if image, ok := child.(string); ok {
				if key == "image" && image != "" {
					found[helpers.ImageName(image)] = struct{}{}
				}
				continue
			}
			collectImages(child, found)
		}
	case []any:
		for _, item := range value {
			collectImages(item, found)
		}
	}
}
