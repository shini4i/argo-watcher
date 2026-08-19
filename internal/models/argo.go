package models

import (
	"fmt"
	"strings"
	"time"

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
	// skipImageValidationAnnotation opts out of the desired-state image check for apps
	// whose images it cannot see: used only by sync hooks (ArgoCD omits those resources),
	// or named by a custom resource whose workload an operator creates out-of-band.
	skipImageValidationAnnotation = "argo-watcher/skip-image-validation"
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
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// Status is the resource's current sync state (example: OutOfSync). ArgoCD omits it for
	// resources it does not compare, so an empty value means "not assessed", not "in sync".
	Status string `json:"status"`
	// RequiresPruning is set for a resource that exists only in the cluster: it reaches Synced
	// by being deleted, which a sync without prune enabled never does.
	RequiresPruning bool `json:"requiresPruning"`
	Health          struct {
		Message string `json:"message"`
		Status  string `json:"status"`
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
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Health    struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"health"`
}

// ApplicationCondition is an entry of ArgoCD's status.conditions: either an error (a "*Error"
// type) or an advisory warning (a "*Warning" type). A manifest-generation failure surfaces here
// and nowhere in the sync status itself, which merely reports "Unknown".
type ApplicationCondition struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type ApplicationStatus struct {
	Health struct {
		Status string `json:"status"`
	}
	Conditions     []ApplicationCondition          `json:"conditions"`
	OperationState ApplicationStatusOperationState `json:"operationState"`
	Resources      []ApplicationResource           `json:"resources"`
	Summary        struct {
		Images []string `json:"images"`
	}
	Sync struct {
		Status string `json:"status"`
		// Revision is the revision the running comparison was performed against. Multi-source
		// applications leave it empty and report one entry per source in Revisions instead.
		Revision  string   `json:"revision"`
		Revisions []string `json:"revisions"`
	}
}

type ApplicationStatusOperationState struct {
	Phase      string `json:"phase"`
	Message    string `json:"message"`
	SyncResult struct {
		Resources []ApplicationOperationResource `json:"resources"`
		// Revision is the revision this sync applied, which is not necessarily the one the
		// application is compared against now (see ApplicationStatus.Sync.Revision).
		Revision  string   `json:"revision"`
		Revisions []string `json:"revisions"`
	} `json:"syncResult"`
}

type ApplicationMetadata struct {
	Name        string            `json:"name"`
	Annotations map[string]string `json:"annotations"`
}

type ApplicationSpec struct {
	Source     ApplicationSource      `json:"source"`
	Sources    []ApplicationSource    `json:"sources"`
	SyncPolicy *ApplicationSyncPolicy `json:"syncPolicy"`
}

// ApplicationSyncPolicy mirrors spec.syncPolicy. A missing Automated block means ArgoCD applies
// the desired state only when a sync is triggered, so drift persists until someone acts.
type ApplicationSyncPolicy struct {
	Automated *ApplicationSyncPolicyAutomated `json:"automated"`
}

type ApplicationSyncPolicyAutomated struct {
	Prune    bool `json:"prune"`
	SelfHeal bool `json:"selfHeal"`
	// Enabled is ArgoCD's explicit opt-out: an automated block with enabled=false is inactive.
	// A nil pointer means enabled, since the field is absent from every policy written before
	// upstream introduced it.
	Enabled *bool `json:"enabled"`
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

// RolloutFailureHeadline renders the first line of a deployment-failure report: what argo-watcher
// observed, and for a drifted application how long it waited before giving up (zero omits the
// duration). A "not synced" failure names ArgoCD's own sync status rather than the internal rollout
// status, because "not synced" alongside a succeeded sync operation reads as a contradiction.
func (app *Application) RolloutFailureHeadline(status string, waited time.Duration) string {
	if status != ArgoRolloutAppNotSynced {
		return fmt.Sprintf("Application deployment failed. Rollout status is %s", status)
	}

	syncStatus := app.Status.Sync.Status
	if syncStatus == "" {
		syncStatus = "unknown"
	}

	headline := fmt.Sprintf("Deployment failed: ArgoCD reports sync status %s", syncStatus)
	// Anything under a second rounds to "0s", which reads as a bug rather than as a fast failure.
	if rounded := waited.Round(time.Second); rounded >= time.Second {
		headline += fmt.Sprintf(" after waiting %s", rounded)
	}
	return headline + "."
}

// GetRolloutMessage generates a rollout failure message.
//
// tree is ArgoCD's live resource tree (optional, may be nil). When present it is the
// preferred source for the "Unhealthy resources" section because it alone carries the
// pod-level failure cause (ImagePullBackOff / CrashLoopBackOff); when nil the message
// falls back to the app's top-level Status.Resources, preserving the pre-tree behaviour.
// The actionable diagnostics (terminal sync operation, failed resources, unhealthy resources, and —
// while the application is still Progressing — the resources that never became ready) are appended
// to both the "not available" and "not healthy"/"degraded" failures so on-call users don't have to
// context-switch into the ArgoCD UI regardless of how the rollout failed. The "not synced" failure
// is reported on its own terms instead: it fails on current drift, which the health-oriented
// sections cannot describe (see buildSyncFailureReport).
func (app *Application) GetRolloutMessage(status string, rolloutImages []string, tree *ApplicationTree) string {
	switch status {
	case ArgoRolloutAppNotAvailable:
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
	case ArgoRolloutAppNotSynced:
		return app.buildSyncFailureReport(tree)
	case ArgoRolloutAppNotHealthy, ArgoRolloutAppDegraded:
		// Appends the same diagnostics as the "not available" path — a stalled rollout
		// caused by a failing pod surfaces here just as often, and its cause lives in the tree.
		base := fmt.Sprintf(
			"App sync status \"%s\"\n"+
				"App health status \"%s\"",
			app.Status.Sync.Status,
			app.Status.Health.Status,
		)
		// The resource listing already covers the top-level resources, so the diagnostics must NOT
		// fall back to them again; only tree-sourced problem nodes (the pod cause) add signal.
		return appendDiagnostics(
			appendResourceListing(base, app.ListUnhealthyResources()),
			app.buildFailureDiagnostics(tree, false),
		)
	}

	return fmt.Sprintf(
		"received unexpected rollout status \"%s\"",
		status,
	)
}

// appendResourceListing appends the "Resources:" block to a failure message, and only when there
// is something to list. ArgoCD reports no health for kinds it cannot assess and a sync can report
// no resources at all, so the listing is routinely empty — and a bare heading with nothing under
// it reads as a failure to collect the diagnostics rather than as "nothing to report".
func appendResourceListing(base string, resources []string) string {
	if len(resources) == 0 {
		return base
	}
	return base + "\nResources:\n\t" + strings.Join(resources, "\n\t")
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

// syncResultOutcome picks the field that describes what happened to a sync-result resource.
//
// A sync status other than "Synced" is the most specific answer there is (SyncFailed, PruneSkipped).
// Failing that, a terminal phase is preferred, because gitops-engine reports a resource whose live
// object went Degraded mid-sync as phase "Failed" while leaving its sync status at "Synced" — and
// "Synced" inside a list of failures reads as a contradiction. The sync status comes next, because
// that same engine reports a *successful* apply as phase "Running", which reads as a resource stuck
// mid-rollout when nothing is wrong. Any field can be absent: a hook that failed its dry run
// carries a sync status but no phase at all.
func syncResultOutcome(r ApplicationOperationResource) string {
	switch {
	case r.Status != "" && r.Status != "Synced":
		return r.Status
	case isTerminalFailurePhase(r.HookPhase):
		return r.HookPhase
	case r.Status != "":
		return r.Status
	default:
		return r.HookPhase
	}
}

// formatSyncResultResource renders a single sync-result resource line: the hook type when the
// resource is one, then the field that describes its outcome. Absent fields are skipped rather
// than rendered as blanks.
func formatSyncResultResource(r ApplicationOperationResource) string {
	var outcome []string

	if r.HookType != "" {
		outcome = append(outcome, r.HookType)
	}
	if described := syncResultOutcome(r); described != "" {
		outcome = append(outcome, described)
	}

	line := fmt.Sprintf("%s(%s)", r.Kind, r.Name)
	if len(outcome) > 0 {
		line += " " + strings.Join(outcome, " ")
	}
	return withMessage(line, r.Message)
}

// withMessage appends a resource's own message to its rendered line. Every resource formatter goes
// through it, so a resource that carries no message never trails an empty "with message" and all
// three listings of the failure report read the same way.
func withMessage(line, message string) string {
	if message == "" {
		return line
	}
	return line + " with message " + message
}

// formatHealthResource renders a single health-bearing resource line. Shared between full and filtered listings
// so the user-facing failure-report format stays consistent if it ever changes.
func formatHealthResource(r ApplicationResource) string {
	return withMessage(fmt.Sprintf("%s(%s) %s", r.Kind, r.Name, r.Health.Status), r.Health.Message)
}

// ListUnhealthyResources returns one formatted line per resource with a non-empty health status.
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

// listOutOfSyncResources returns one formatted line per top-level resource whose sync status is
// not "Synced". Resources ArgoCD does not compare carry no status at all and are skipped: an empty
// value means "not assessed" rather than "drifted", and reporting those would bury the real culprit.
// A resource that only reaches Synced by being deleted is annotated, because a sync without prune
// enabled will never do that and waiting cannot resolve it.
func (app *Application) listOutOfSyncResources() []string {
	var list []string

	for index := range app.Status.Resources {
		resource := app.Status.Resources[index]
		if resource.Status == "" || resource.Status == "Synced" {
			continue
		}
		line := fmt.Sprintf("%s(%s) %s", resource.Kind, resource.Name, resource.Status)
		if resource.RequiresPruning {
			line += " (requires pruning)"
		}
		list = append(list, line)
	}
	return list
}

// listErrorConditions returns one formatted line per application condition reporting an error.
// ArgoCD names every error condition with an "Error" suffix and every advisory one with a
// "Warning" suffix, so the suffix decides what is actionable — a fixed list would go stale as
// upstream adds condition types.
func (app *Application) listErrorConditions() []string {
	var list []string

	for index := range app.Status.Conditions {
		condition := app.Status.Conditions[index]
		if !strings.HasSuffix(condition.Type, "Error") {
			continue
		}
		list = append(list, fmt.Sprintf("%s: %s", condition.Type, condition.Message))
	}
	return list
}

// buildSyncFailureReport renders the whole "not synced" failure report. The failure is decided by
// the application's *current* sync status, while the last sync *operation* it ran routinely
// succeeded — reporting the operation first made the two read as a contradiction, so the report
// leads with why the application is still out of sync and closes with the operation as context.
// Every section but the last is included only when it has content.
func (app *Application) buildSyncFailureReport(tree *ApplicationTree) string {
	var sections []string

	if explanation := app.buildDriftExplanation(); explanation != "" {
		sections = append(sections, explanation)
	}

	// Errors come before the listings: the resource lists are unbounded, so the one line naming
	// the cause must not trail dozens of drift lines.
	if conditions := app.listErrorConditions(); len(conditions) > 0 {
		sections = append(sections, "Sync errors:\n\t"+strings.Join(conditions, "\n\t"))
	}

	// GetRolloutStatus classifies a Degraded application that is still OutOfSync as "not synced",
	// because a pending sync may yet recover it. This report is therefore the only one such a
	// failure ever produces, and the pod-level cause of the degradation reaches the user only from
	// the tree — Status.Resources carries no pod health.
	if tree != nil {
		if problems := tree.ListProblemNodes(); len(problems) > 0 {
			sections = append(sections, "Unhealthy resources:\n\t"+strings.Join(problems, "\n\t"))
		}
	}

	if resources := app.listOutOfSyncResources(); len(resources) > 0 {
		sections = append(sections, "Out-of-sync resources:\n\t"+strings.Join(resources, "\n\t"))
	}

	if failed := app.listFailedSyncResultResources(); len(failed) > 0 {
		sections = append(sections, "Failed resources:\n\t"+strings.Join(failed, "\n\t"))
	}

	return strings.Join(append(sections, app.lastSyncOperationLine()), "\n\n")
}

// buildDriftExplanation explains why an application whose last sync succeeded is still out of sync,
// which is the question the report exists to answer. It compares the revision that sync applied
// against the revision the running comparison uses: a different one means the desired state moved,
// the same one means applying it did not converge the live state. It returns an empty string when
// ArgoCD reported no revisions, or when the last sync did not succeed — matching revisions then mean
// nothing was applied, not that applying it failed to stick.
//
// The wording deliberately claims no more than the two fields establish. Which revision is newer in
// git history is not among it: a rollback, a revert or a retargeted branch all leave the comparison
// pointing at an older revision.
func (app *Application) buildDriftExplanation() string {
	if app.Status.OperationState.Phase != "Succeeded" {
		return ""
	}

	applied := newRevisions(app.Status.OperationState.SyncResult.Revision, app.Status.OperationState.SyncResult.Revisions)
	compared := newRevisions(app.Status.Sync.Revision, app.Status.Sync.Revisions)
	if applied.key == "" || compared.key == "" {
		return ""
	}

	var explanation string
	if applied.key == compared.key {
		explanation = fmt.Sprintf(
			"The last sync succeeded for %s and the application is still out of sync against exactly "+
				"what that sync applied, so applying it did not converge the live state. Usual causes: "+
				"a mutating admission webhook, a controller that owns a field the manifests also set "+
				"(replicas versus an HPA), a resource that has to be pruned, or a sync that covered "+
				"only some resources. Extra waiting is unlikely to help.",
			applied.phrase,
		)
	} else {
		explanation = fmt.Sprintf(
			"The last sync applied %s, but ArgoCD now compares the application against %s, so the "+
				"desired state has changed since that sync.",
			applied.phrase, compared.phrase,
		)
	}

	return explanation + "\n" + app.autoSyncNote()
}

// autoSyncNote states whether ArgoCD will act on the drift by itself, which decides whether the
// operator has to. It accompanies the drift explanation rather than standing alone: on its own it
// would add a line to every report that has nothing to explain.
func (app *Application) autoSyncNote() string {
	if !app.AutoSyncEnabled() {
		return "Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered."
	}

	automated := app.Spec.SyncPolicy.Automated
	return fmt.Sprintf("Auto-sync is enabled (prune %s, self-heal %s).",
		onOff(automated.Prune), onOff(automated.SelfHeal))
}

// lastSyncOperationLine reports the outcome of the sync ArgoCD last ran. Unconditional, so a
// report whose every other section emptied out still says something.
func (app *Application) lastSyncOperationLine() string {
	operation := app.Status.OperationState
	if operation.Phase == "" && operation.Message == "" {
		return "No sync operation is recorded for this application."
	}

	phase := operation.Phase
	if phase == "" {
		phase = "unknown"
	}

	line := "Last sync operation: " + phase
	if operation.Message != "" {
		line += fmt.Sprintf(", message: %q", operation.Message)
	}
	return line
}

// AutoSyncEnabled reports whether ArgoCD applies this application's desired state on its own.
func (app *Application) AutoSyncEnabled() bool {
	if app.Spec.SyncPolicy == nil || app.Spec.SyncPolicy.Automated == nil {
		return false
	}
	return app.Spec.SyncPolicy.Automated.Enabled == nil || *app.Spec.SyncPolicy.Automated.Enabled
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

// revisions is one of ArgoCD's revision reports: key is the untruncated value two reports are
// compared by, phrase is how the failure message names them. Keeping the two apart matters — two
// distinct commits can share an abbreviated prefix, and comparing the abbreviations would hand the
// user the wrong verdict. An empty key means ArgoCD reported no revision at all.
type revisions struct {
	key    string
	phrase string
}

// newRevisions reads a revision report from ArgoCD's pair of fields: a single-source application
// fills revision, a multi-source one fills list with one entry per source. The list wins whenever
// it holds more than one entry, so an application that reports both cannot have its verdict decided
// by one source's revision while another source is the one that moved.
func newRevisions(revision string, list []string) revisions {
	if revision != "" && len(list) <= 1 {
		return revisions{key: revision, phrase: "revision " + shortRevision(revision)}
	}
	if len(list) == 0 {
		return revisions{}
	}

	short := make([]string, len(list))
	for index := range list {
		short[index] = shortRevision(list[index])
	}

	label := "revision "
	if len(list) > 1 {
		label = "revisions "
	}
	return revisions{key: strings.Join(list, ","), phrase: label + strings.Join(short, ", ")}
}

// shortRevision abbreviates a full git SHA to the seven characters ArgoCD's own UI shows. Anything
// else — a tag, a branch, a chart version — is returned untouched.
func shortRevision(revision string) string {
	const shaLength = 40

	if len(revision) != shaLength {
		return revision
	}
	for _, char := range revision {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return revision
		}
	}
	return revision[:7]
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

// listFailedSyncResultResources returns formatted lines for the sync-result resources that did not
// go through: a hook that ended in a terminal failure phase, or an ordinary resource ArgoCD could
// not apply. The rest of the sync result describes work that succeeded and is left out.
func (app *Application) listFailedSyncResultResources() []string {
	var list []string
	for index := range app.Status.OperationState.SyncResult.Resources {
		resource := app.Status.OperationState.SyncResult.Resources[index]
		if !isTerminalFailurePhase(resource.HookPhase) && resource.Status != "SyncFailed" {
			continue
		}
		list = append(list, formatSyncResultResource(resource))
	}
	return list
}

// listProblemResources returns formatted lines for resources whose health status indicates a problem.
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
	return withMessage(fmt.Sprintf("%s(%s) %s", n.Kind, n.Name, n.Health.Status), n.Health.Message)
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

// ListProgressingNodes returns formatted lines for resource-tree nodes still Progressing — the
// resources a rollout that ran out its timeout was still waiting on.
func (tree *ApplicationTree) ListProgressingNodes() []string {
	var list []string
	for index := range tree.Nodes {
		node := tree.Nodes[index]
		if node.Health.Status != "Progressing" {
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

	if failed := app.listFailedSyncResultResources(); len(failed) > 0 {
		sections = append(sections, "Failed resources:\n\t"+strings.Join(failed, "\n\t"))
	}

	if resources := app.problemResourceLines(tree, allowStatusFallback); len(resources) > 0 {
		sections = append(sections, "Unhealthy resources:\n\t"+strings.Join(resources, "\n\t"))
	}

	// A still-Progressing app ran out its timeout while coming up, and isProblemHealthStatus
	// excludes Progressing, so without this the failure can name no resource at all. Gated on the
	// app's own health rather than on the absence of problem resources: a Suspended CronJob or a
	// Missing resource is a problem node that is NOT the reason the rollout stalled, and it must
	// not hide the workload that never became ready. A Degraded app reports health "Degraded", so
	// its report stays free of progressing noise.
	if tree != nil && app.Status.Health.Status == "Progressing" {
		if pending := tree.ListProgressingNodes(); len(pending) > 0 {
			sections = append(sections, "Resources still progressing:\n\t"+strings.Join(pending, "\n\t"))
		}
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

// IsImageValidationSkipped reports whether the app carries "argo-watcher/skip-image-validation=true".
func (app *Application) IsImageValidationSkipped() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[skipImageValidationAnnotation] == "true"
}

type Userinfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username"`
}
