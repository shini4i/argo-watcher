package models

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/pkg/updater"
	"strings"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

const (
	ArgoRolloutAppSuccess      = "success"
	ArgoRolloutAppNotSynced    = "not synced"
	ArgoRolloutAppNotAvailable = "not available"
	ArgoRolloutAppNotHealthy   = "not healthy"
)

type ApplicationOperationResource struct {
	HookPhase string `json:"hookPhase"` // example: Failed
	HookType  string `json:"hookType"`  // example: PreSync
	Kind      string `json:"kind"`      // example: Pod | Job
	Message   string `json:"message"`   // example: Job has reached the specified backoff limit
	Status    string `json:"status"`    // example: Synced
	SyncPhase string `json:"syncPhase"` // example: PreSync
	Name      string `json:"name"`      // example: app-migrations
	Namespace string `json:"namespace"` // example: app
}

type ApplicationResource struct {
	Kind      string `json:"kind"`      // example: Pod | Job
	Name      string `json:"name"`      // example: app-migrations
	Namespace string `json:"namespace"` // example: app
	Health    struct {
		Message string `json:"message"` // example: Job has reached the specified backoff limit
		Status  string `json:"status"`  // example: Synced
	} `json:"health"`
}

type Application struct {
	Metadata struct {
		Name        string            `json:"name"`
		Annotations map[string]string `json:"annotations"`
	}
	Spec struct {
		Source struct {
			RepoURL        string `json:"repoURL"`
			TargetRevision string `json:"targetRevision"`
			Path           string `json:"path"`
		}
	}
	Status struct {
		Health struct {
			Status string `json:"status"`
		}
		OperationState struct {
			Phase      string `json:"phase"`
			Message    string `json:"message"`
			SyncResult struct {
				Resources []ApplicationOperationResource `json:"resources"`
			} `json:"syncResult"`
		} `json:"operationState"`
		Resources []ApplicationResource `json:"resources"`
		Summary   struct {
			Images []string `json:"images"`
		}
		Sync struct {
			Status string `json:"status"`
		}
	} `json:"status"`
}

// GetRolloutStatus calculates application rollout status depending on the expected images and proxy configuration.
func (app *Application) GetRolloutStatus(rolloutImages []string, registryProxyUrl string) string {
	// check if all the images rolled out
	for _, image := range rolloutImages {
		if !helpers.ImagesContains(app.Status.Summary.Images, image, registryProxyUrl) {
			return ArgoRolloutAppNotAvailable
		}
	}

	// verify app sync status
	if app.Status.Sync.Status != "Synced" {
		return ArgoRolloutAppNotSynced
	}

	// verify app health status
	if app.Status.Health.Status != "Healthy" {
		return ArgoRolloutAppNotHealthy
	}

	// all good
	return ArgoRolloutAppSuccess
}

// GetRolloutMessage generates rollout failure message.
func (app *Application) GetRolloutMessage(status string, rolloutImages []string) string {
	// handle application sync failure
	switch status {
	// not all images were deployed to the application
	case ArgoRolloutAppNotAvailable:
		// define details
		return fmt.Sprintf(
			"List of current images (last app check):\n"+
				"\t%s\n\n"+
				"List of expected images:\n"+
				"\t%s",
			strings.Join(app.Status.Summary.Images, "\n\t"),
			strings.Join(rolloutImages, "\n\t"),
		)
	// application sync status wasn't valid
	case ArgoRolloutAppNotSynced:
		// display sync status and last sync message
		return fmt.Sprintf(
			"App status \"%s\"\n"+
				"App message \"%s\"\n"+
				"Resources:\n"+
				"\t%s",
			app.Status.OperationState.Phase,
			app.Status.OperationState.Message,
			strings.Join(app.ListSyncResultResources(), "\n\t"),
		)
	// application is not in a healthy status
	case ArgoRolloutAppNotHealthy:
		// display current health of pods
		return fmt.Sprintf(
			"App sync status \"%s\"\n"+
				"App health status \"%s\"\n"+
				"Resources:\n"+
				"\t%s",
			app.Status.Sync.Status,
			app.Status.Health.Status,
			strings.Join(app.ListUnhealthyResources(), "\n\t"),
		)
	}

	// handle unexpected status
	return fmt.Sprintf(
		"received unexpected rollout status \"%s\"",
		status,
	)
}

// IsFinalRolloutStatus checks if rollout status is final.
func (app *Application) IsFinalRolloutStatus(status string) bool {
	return status == ArgoRolloutAppSuccess
}

// ListSyncResultResources returns a list of strings representing the sync result resources of the application.
// Each string in the list contains information about the resource's kind, name, hook type, hook phase, and message.
// The information is formatted as "{kind}({name}) {hookType} {hookPhase} with message {message}".
// The list is generated based on the Application's status and its operation state's sync result resources.
func (app *Application) ListSyncResultResources() []string {
	list := make([]string, len(app.Status.OperationState.SyncResult.Resources))
	for index := range app.Status.OperationState.SyncResult.Resources {
		resource := app.Status.OperationState.SyncResult.Resources[index]
		list[index] = fmt.Sprintf("%s(%s) %s %s with message %s", resource.Kind, resource.Name, resource.HookType, resource.HookPhase, resource.Message)
	}
	return list
}

// ListUnhealthyResources returns a list of strings representing the unhealthy resources of the application.
// Each string in the list contains information about the resource's kind, name, and health status.
// If available, the resource's health message is also included in the string.
// The format of each string is "{kind}({name}) {status}" or "{kind}({name}) {status} with message {message}".
// The list is generated based on the Application's status and its resources with non-empty health status.
func (app *Application) ListUnhealthyResources() []string {
	var list []string

	for index := range app.Status.Resources {
		resource := app.Status.Resources[index]
		if resource.Health.Status == "" {
			continue
		}
		message := fmt.Sprintf("%s(%s) %s", resource.Kind, resource.Name, resource.Health.Status)
		if resource.Health.Message != "" {
			message += " with message " + resource.Health.Message
		}
		list = append(list, message)
	}
	return list
}

// IsManagedByWatcher checks if the application is managed by the watcher.
// It checks if the application's metadata contains the "argo-watcher/managed" annotation with the value "true".
func (app *Application) IsManagedByWatcher() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations["argo-watcher/managed"] == "true"
}

func (app *Application) processAppAnnotations(annotations map[string]string, task *Task) *updater.ArgoOverrideFile {
	overrideFileContent := updater.ArgoOverrideFile{}
	managedImages := extractManagedImages(annotations)

	if len(managedImages) == 0 {
		log.Error().Msgf("argo-watcher/managed-images annotation not found, skipping image update")
		return nil
	}

	for _, image := range task.Images {
		for appAlias, appImage := range managedImages {
			if image.Image == appImage {
				if tagPath, exists := annotations[fmt.Sprintf("argo-watcher/%s.helm.image-tag", appAlias)]; exists {
					overrideFileContent.Helm.Parameters = append(overrideFileContent.Helm.Parameters, updater.ArgoParameterOverride{
						Name:        tagPath,
						Value:       image.Tag,
						ForceString: true,
					})
				} else {
					log.Error().Msgf("argo-watcher/%s.helm.image-tag annotation not found, skipping image %s update", appAlias, image.Image)
				}
			}
		}
	}

	return &overrideFileContent
}

func (app *Application) UpdateGitImageTag(task *Task) {
	if app.Spec.Source.Path == "" {
		log.Warn().Str("id", task.Id).Msgf("No path found for app %s, unsupported Application configuration", app.Metadata.Name)
		return
	}

	releaseOverrides := app.processAppAnnotations(app.Metadata.Annotations, task)
	if releaseOverrides == nil {
		log.Warn().Str("id", task.Id).Msgf("No release overrides found for app %s", app.Metadata.Name)
		return
	}

	git := updater.GitRepo{RepoURL: app.Spec.Source.RepoURL, BranchName: app.Spec.Source.TargetRevision, Path: app.Spec.Source.Path}
	if err := git.Clone(); err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to clone git repository %s", app.Spec.Source.RepoURL)
		return
	}

	if err := git.UpdateApp(app.Metadata.Name, releaseOverrides); err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to update git repository %s", app.Spec.Source.RepoURL)
		return
	}
}

// extractManagedImages extracts the managed images from the application's annotations.
// It returns a map of the managed images, where the key is the application alias and the value is the image name.
func extractManagedImages(annotations map[string]string) map[string]string {
	managedImages := map[string]string{}

	for annotation, value := range annotations {
		if annotation == "argo-watcher/managed-images" {
			for _, image := range strings.Split(value, ",") {
				managedImages[strings.Split(image, "=")[0]] = strings.Split(image, "=")[1]
			}
		}
	}

	return managedImages
}

type Userinfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username"`
}
