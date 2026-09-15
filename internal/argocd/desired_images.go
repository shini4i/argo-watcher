package argocd

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/shini4i/argo-watcher/internal/models"
)

// desiredImageNames returns the sorted, de-duplicated repository names (tags and digests
// stripped) of every container image declared in the application's rendered manifests. An
// undecodable manifest is an error, not a skip: it may be the one declaring the image the
// caller looks for, and a shorter list reads as proof of an absence.
func desiredImageNames(rendered *models.ApplicationManifests) ([]string, error) {
	if rendered == nil {
		return nil, nil
	}

	found := make(map[string]struct{})

	for index := range rendered.Manifests {
		manifestJSON := rendered.Manifests[index]
		if manifestJSON == "" {
			continue
		}

		var manifest any
		if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
			return nil, fmt.Errorf("decoding manifest %d: %w", index, err)
		}

		collectImages(manifest, found)
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
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
