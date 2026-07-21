package models

import (
	"fmt"
	"strings"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

const (
	ArgoRolloutAppSuccess      = "success"
	ArgoRolloutAppNotSynced    = "not synced"
	ArgoRolloutAppNotAvailable = "not available"
	ArgoRolloutAppNotHealthy   = "not healthy"
	ArgoRolloutAppDegraded     = "degraded"
)

const (
	managedAnnotation       = "argo-watcher/managed"
	managedGitRepo          = "argo-watcher/write-back-repo"
	managedGitBranch        = "argo-watcher/write-back-branch"
	managedGitPath          = "argo-watcher/write-back-path"
	managedGitFile          = "argo-watcher/write-back-filename"
	fireAndForgetAnnotation = "argo-watcher/fire-and-forget"
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
	Metadata ApplicationMetadata `json:"metadata"`
	Spec     ApplicationSpec     `json:"spec"`
	Status   ApplicationStatus   `json:"status"`
}

// ApplicationTree is the live resource tree returned by ArgoCD's
// /api/v1/applications/{name}/resource-tree endpoint. Unlike Application.Status.Resources
// (which lists only the app's top-level managed resources: Deployment, Service, ...), the
// tree includes their descendants — crucially the Pods, whose health carries the actual
// failure cause (ImagePullBackOff, CrashLoopBackOff) that the top-level resources never expose.
type ApplicationTree struct {
	Nodes []ApplicationTreeNode `json:"nodes"`
}

type ApplicationTreeNode struct {
	Kind      string `json:"kind"`      // example: Pod | Job
	Name      string `json:"name"`      // example: app-6b7f-abcde
	Namespace string `json:"namespace"` // example: app
	Health    struct {
		Status  string `json:"status"`  // example: Degraded
		Message string `json:"message"` // example: Back-off pulling image "app:v2": ErrImagePull
	} `json:"health"`
}

type ApplicationStatus struct {
	Health struct {
		Status string `json:"status"`
	}
	OperationState ApplicationStatusOperationState `json:"operationState"`
	Resources      []ApplicationResource           `json:"resources"`
	Summary        struct {
		Images []string `json:"images"`
	}
	Sync struct {
		Status string `json:"status"`
	}
}

type ApplicationStatusOperationState struct {
	Phase      string `json:"phase"`
	Message    string `json:"message"`
	SyncResult struct {
		Resources []ApplicationOperationResource `json:"resources"`
	} `json:"syncResult"`
}

type ApplicationMetadata struct {
	Name        string            `json:"name"`
	Annotations map[string]string `json:"annotations"`
}

type ApplicationSpec struct {
	Source  ApplicationSource   `json:"source"`
	Sources []ApplicationSource `json:"sources"`
}

type ApplicationSource struct {
	RepoURL        string `json:"repoURL"`
	TargetRevision string `json:"targetRevision"`
	Path           string `json:"path"`
}

// GetRolloutStatus calculates application rollout status depending on the expected images and proxy configuration.
func (app *Application) GetRolloutStatus(rolloutImages []string, registryProxyUrl string, acceptSuspended bool) string {
	for _, image := range rolloutImages {
		if !helpers.ImagesContains(app.Status.Summary.Images, image, registryProxyUrl) {
			return ArgoRolloutAppNotAvailable
		}
	}

	// A degraded app is terminal and worth reporting, unless it is also OutOfSync
	// (a sync is still pending and may recover).
	if app.Status.Health.Status == "Degraded" && app.Status.Sync.Status != "OutOfSync" {
		return ArgoRolloutAppDegraded
	}

	if app.Status.Sync.Status != "Synced" {
		return ArgoRolloutAppNotSynced
	}

	// A Rollout object can sit in Suspended mid-rollout; treat that as success when
	// the operator opted in via acceptSuspended.
	if app.Status.Health.Status == "Suspended" && app.Status.Sync.Status == "Synced" && acceptSuspended {
		return ArgoRolloutAppSuccess
	}

	if app.Status.Health.Status != "Healthy" {
		return ArgoRolloutAppNotHealthy
	}

	return ArgoRolloutAppSuccess
}

// GetRolloutMessage generates a rollout failure message.
//
// tree is ArgoCD's live resource tree (optional, may be nil). When present it is the
// preferred source for the "Unhealthy resources" section because it alone carries the
// pod-level failure cause (ImagePullBackOff / CrashLoopBackOff); when nil the message
// falls back to the app's top-level Status.Resources, preserving the pre-tree behaviour.
// The actionable diagnostics (terminal sync operation, failed hooks, unhealthy resources)
// are appended to both the "not available" and "not healthy"/"degraded" failures so on-call
// users don't have to context-switch into the ArgoCD UI regardless of how the rollout failed.
func (app *Application) GetRolloutMessage(status string, rolloutImages []string, tree *ApplicationTree) string {
	switch status {
	// not all expected images were deployed
	case ArgoRolloutAppNotAvailable:
		// Image-mismatch is the bare minimum we always show, then append any diagnostics.
		base := fmt.Sprintf(
			"List of current images (last app check):\n"+
				"\t%s\n\n"+
				"List of expected images:\n"+
				"\t%s",
			strings.Join(app.Status.Summary.Images, "\n\t"),
			strings.Join(rolloutImages, "\n\t"),
		)
		// Base message has no resource listing, so fall back to Status.Resources when no tree.
		return appendDiagnostics(base, app.buildFailureDiagnostics(tree, true))
	// sync status was not "Synced"
	case ArgoRolloutAppNotSynced:
		return fmt.Sprintf(
			"App status \"%s\"\n"+
				"App message \"%s\"\n"+
				"Resources:\n"+
				"\t%s",
			app.Status.OperationState.Phase,
			app.Status.OperationState.Message,
			strings.Join(app.ListSyncResultResources(), "\n\t"),
		)
	// app is unhealthy or degraded
	case ArgoRolloutAppNotHealthy, ArgoRolloutAppDegraded:
		// display current health of the top-level resources, then append the same
		// diagnostics as the "not available" path — a stalled rollout caused by a
		// failing pod surfaces here just as often, and its cause lives in the tree.
		base := fmt.Sprintf(
			"App sync status \"%s\"\n"+
				"App health status \"%s\"\n"+
				"Resources:\n"+
				"\t%s",
			app.Status.Sync.Status,
			app.Status.Health.Status,
			strings.Join(app.ListUnhealthyResources(), "\n\t"),
		)
		// Base "Resources:" already lists the top-level resources, so do NOT fall back to them
		// again here; only tree-sourced problem nodes (the distinct pod cause) add signal.
		return appendDiagnostics(base, app.buildFailureDiagnostics(tree, false))
	}

	return fmt.Sprintf(
		"received unexpected rollout status \"%s\"",
		status,
	)
}

// appendDiagnostics joins the base failure message with the optional diagnostics suffix,
// separated by a blank line. An empty suffix leaves the base message byte-identical, so
// failures with no extra diagnostics keep their historical format.
func appendDiagnostics(base, diagnostics string) string {
	if diagnostics == "" {
		return base
	}
	return base + "\n\n" + diagnostics
}

// formatSyncResultResource renders a single sync-result resource line. Shared between full and filtered listings
// so the user-facing failure-report format stays consistent if it ever changes.
func formatSyncResultResource(r ApplicationOperationResource) string {
	return fmt.Sprintf("%s(%s) %s %s with message %s", r.Kind, r.Name, r.HookType, r.HookPhase, r.Message)
}

// ListSyncResultResources returns one formatted line per sync-result resource
// (see formatSyncResultResource for the format).
func (app *Application) ListSyncResultResources() []string {
	list := make([]string, len(app.Status.OperationState.SyncResult.Resources))
	for index := range app.Status.OperationState.SyncResult.Resources {
		list[index] = formatSyncResultResource(app.Status.OperationState.SyncResult.Resources[index])
	}
	return list
}

// formatHealthResource renders a single health-bearing resource line. Shared between full and filtered listings
// so the user-facing failure-report format stays consistent if it ever changes.
func formatHealthResource(r ApplicationResource) string {
	line := fmt.Sprintf("%s(%s) %s", r.Kind, r.Name, r.Health.Status)
	if r.Health.Message != "" {
		line += " with message " + r.Health.Message
	}
	return line
}

// ListUnhealthyResources returns one formatted line per resource with a non-empty
// health status (see formatHealthResource for the format).
func (app *Application) ListUnhealthyResources() []string {
	var list []string

	for index := range app.Status.Resources {
		resource := app.Status.Resources[index]
		if resource.Health.Status == "" {
			continue
		}
		list = append(list, formatHealthResource(resource))
	}
	return list
}

// isTerminalFailurePhase reports whether the given ArgoCD phase value indicates a terminal failure. The same
// predicate is applied to both Status.OperationState.Phase (OperationPhase) and SyncResult.Resources[].HookPhase
// (HookPhase); upstream defines these as separate string enums that share the same value set.
// "Running", "Succeeded", "Terminating", and the empty string are deliberately excluded.
func isTerminalFailurePhase(phase string) bool {
	return phase == "Failed" || phase == "Error"
}

// isProblemHealthStatus reports whether an ArgoCD resource HealthStatusCode indicates a problem worth surfacing
// in the deployment failure report. "Healthy" and "Progressing" are excluded so they don't dilute the signal;
// "Synced" appears in legacy fixtures and is treated as non-actionable.
func isProblemHealthStatus(status string) bool {
	switch status {
	case "Degraded", "Missing", "Unknown", "Suspended":
		return true
	default:
		return false
	}
}

// listFailedSyncResultResources returns formatted lines for sync-result resources whose hook phase indicates failure.
// Reuses the same one-line format as ListSyncResultResources via formatSyncResultResource.
func (app *Application) listFailedSyncResultResources() []string {
	var list []string
	for index := range app.Status.OperationState.SyncResult.Resources {
		resource := app.Status.OperationState.SyncResult.Resources[index]
		if !isTerminalFailurePhase(resource.HookPhase) {
			continue
		}
		list = append(list, formatSyncResultResource(resource))
	}
	return list
}

// listProblemResources returns formatted lines for resources whose health status indicates a problem.
// Reuses formatHealthResource for output formatting; applies the stricter problem-only filter via isProblemHealthStatus.
func (app *Application) listProblemResources() []string {
	var list []string
	for index := range app.Status.Resources {
		resource := app.Status.Resources[index]
		if !isProblemHealthStatus(resource.Health.Status) {
			continue
		}
		list = append(list, formatHealthResource(resource))
	}
	return list
}

// formatTreeNode renders a single resource-tree node line, mirroring formatHealthResource so
// tree-sourced and Status.Resources-sourced "Unhealthy resources" lines look identical.
func formatTreeNode(n ApplicationTreeNode) string {
	line := fmt.Sprintf("%s(%s) %s", n.Kind, n.Name, n.Health.Status)
	if n.Health.Message != "" {
		line += " with message " + n.Health.Message
	}
	return line
}

// ListProblemNodes returns formatted lines for resource-tree nodes whose health indicates a
// problem. This is where pod-level failure causes (ImagePullBackOff, CrashLoopBackOff) surface —
// they are carried by the Pod nodes, which never appear in Application.Status.Resources.
func (tree *ApplicationTree) ListProblemNodes() []string {
	var list []string
	for index := range tree.Nodes {
		node := tree.Nodes[index]
		if !isProblemHealthStatus(node.Health.Status) {
			continue
		}
		list = append(list, formatTreeNode(node))
	}
	return list
}

// problemResourceLines returns the "Unhealthy resources" lines. It always prefers the live
// resource tree (which alone carries pod-level causes). When the tree is nil it falls back to
// the app's top-level Status.Resources only if allowStatusFallback is set — the not-available
// path enables the fallback (its base message has no resource listing), while the not-healthy
// path disables it because its base "Resources:" block already lists those same resources.
func (app *Application) problemResourceLines(tree *ApplicationTree, allowStatusFallback bool) []string {
	if tree != nil {
		return tree.ListProblemNodes()
	}
	if allowStatusFallback {
		return app.listProblemResources()
	}
	return nil
}

// buildFailureDiagnostics builds the optional diagnostics suffix appended to the "not available"
// and "not healthy"/"degraded" rollout-failure messages. Each section is included only when it
// has content; the empty string is returned when no diagnostics are available, preserving the
// legacy output for that case. tree is optional (see GetRolloutMessage); allowStatusFallback
// controls whether the tree-less "Unhealthy resources" section falls back to Status.Resources.
func (app *Application) buildFailureDiagnostics(tree *ApplicationTree, allowStatusFallback bool) string {
	var sections []string

	if isTerminalFailurePhase(app.Status.OperationState.Phase) {
		opSection := fmt.Sprintf("Sync operation phase: %s", app.Status.OperationState.Phase)
		if msg := app.Status.OperationState.Message; msg != "" {
			opSection += "\nSync operation message: " + msg
		}
		sections = append(sections, opSection)
	}

	if hooks := app.listFailedSyncResultResources(); len(hooks) > 0 {
		sections = append(sections, "Failed hooks:\n\t"+strings.Join(hooks, "\n\t"))
	}

	if resources := app.problemResourceLines(tree, allowStatusFallback); len(resources) > 0 {
		sections = append(sections, "Unhealthy resources:\n\t"+strings.Join(resources, "\n\t"))
	}

	return strings.Join(sections, "\n\n")
}

// IsManagedByWatcher reports whether the app carries the "argo-watcher/managed=true" annotation.
func (app *Application) IsManagedByWatcher() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[managedAnnotation] == "true"
}

// IsFireAndForgetModeActive checks if 'fire-and-forget' mode is enabled in Application's annotations.
func (app *Application) IsFireAndForgetModeActive() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[fireAndForgetAnnotation] == "true"
}

type Userinfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username"`
}
