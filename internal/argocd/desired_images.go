package argocd

import (
	"encoding/json"
	"slices"

	"github.com/shini4i/argo-watcher/internal/models"
)

// desiredImageNames returns the sorted, de-duplicated repository names (tags and
// digests stripped) of every container image declared in the application's desired
// state. An unreadable item is skipped rather than aborting the walk: ArgoCD marshals
// every target state, so an item that fails to decode carries nothing to read anyway.
func desiredImageNames(resources *models.ManagedResources) []string {
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

// collectImages records the repository name of every string stored under an "image" key, at any
// depth. Matching on the key alone keeps Deployments, CronJobs, Rollouts and image-bearing CRDs
// on one code path. A stray match widens the accepted set, but also makes it non-empty, which is
// what lets desiredImageNames' caller conclude at all.
func collectImages(node any, found map[string]struct{}) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if image, ok := child.(string); ok {
				if key == "image" && image != "" {
					found[imageName(image)] = struct{}{}
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
