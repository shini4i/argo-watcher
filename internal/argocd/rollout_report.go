package argocd

import (
	"fmt"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	ArgoRolloutAppSuccess      = "success"
	ArgoRolloutAppNotSynced    = "not synced"
	ArgoRolloutAppNotAvailable = "not available"
	ArgoRolloutAppNotHealthy   = "not healthy"
	ArgoRolloutAppDegraded     = "degraded"
)

// rolloutStatus derives the rollout status from the images the app runs, the expected images and
// the registry-proxy setting.
func rolloutStatus(app *models.Application, rolloutImages []string, registryProxyUrl string, acceptSuspended bool) string {
	for _, image := range rolloutImages {
		if !imagesContains(app.Status.Summary.Images, image, registryProxyUrl) {
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

// rolloutFailureHeadline renders the first line of a deployment-failure report: what argo-watcher
// observed, and for a drifted application how long it waited before giving up (zero omits the
// duration). A "not synced" failure names ArgoCD's own sync status rather than the internal rollout
// status, because "not synced" alongside a succeeded sync operation reads as a contradiction.
func rolloutFailureHeadline(app *models.Application, status string, waited time.Duration) string {
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

// rolloutMessage renders the failure message for a rollout status. tree (optional) alone
// carries pod-level causes; nil falls back to Status.Resources. Diagnostics are appended to the
// "not available" and "not healthy"/"degraded" failures so nobody has to open the ArgoCD UI;
// "not synced" fails on current drift instead and is rendered by buildSyncFailureReport.
func rolloutMessage(app *models.Application, status string, rolloutImages []string, tree *models.ApplicationTree) string {
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
		return appendDiagnostics(base, buildFailureDiagnostics(app, tree, true))
	case ArgoRolloutAppNotSynced:
		return buildSyncFailureReport(app, tree)
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
			appendResourceListing(base, listUnhealthyResources(app)),
			buildFailureDiagnostics(app, tree, false),
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

// syncResultOutcome picks the field describing what happened to a sync-result resource: a sync
// status other than "Synced" first, then a terminal phase. gitops-engine reports a resource that
// went Degraded mid-sync as phase "Failed" with sync status still "Synced", and a successful
// apply as phase "Running", so neither field alone reads correctly. Either may be absent.
func syncResultOutcome(r models.ApplicationOperationResource) string {
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
func formatSyncResultResource(r models.ApplicationOperationResource) string {
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

// formatHealthResource renders one health-bearing resource line, shared by the full and filtered
// listings so the failure-report format stays consistent.
func formatHealthResource(r models.ApplicationResource) string {
	return withMessage(fmt.Sprintf("%s(%s) %s", r.Kind, r.Name, r.Health.Status), r.Health.Message)
}

// listUnhealthyResources returns one formatted line per resource with a non-empty health status.
func listUnhealthyResources(app *models.Application) []string {
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

// listOutOfSyncResources returns one line per top-level resource whose sync status is not
// "Synced". Resources ArgoCD does not compare carry no status and are skipped: empty means "not
// assessed", not "drifted". A resource that only reaches Synced by deletion is annotated, because
// a sync without prune enabled never gets it there and waiting cannot resolve it.
func listOutOfSyncResources(app *models.Application) []string {
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
func listErrorConditions(app *models.Application) []string {
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

// buildSyncFailureReport renders the "not synced" failure. The failure is decided by the current
// sync status while the last sync operation routinely succeeded, so the report leads with why the
// app is still out of sync and closes with the operation as context. Every section but the last
// is included only when it has content.
func buildSyncFailureReport(app *models.Application, tree *models.ApplicationTree) string {
	var sections []string

	if explanation := buildDriftExplanation(app); explanation != "" {
		sections = append(sections, explanation)
	}

	// Errors come before the listings: the resource lists are unbounded, so the one line naming
	// the cause must not trail dozens of drift lines.
	if conditions := listErrorConditions(app); len(conditions) > 0 {
		sections = append(sections, "Sync errors:\n\t"+strings.Join(conditions, "\n\t"))
	}

	// rolloutStatus classifies a Degraded application that is still OutOfSync as "not synced",
	// because a pending sync may yet recover it. This report is therefore the only one such a
	// failure ever produces, and the pod-level cause of the degradation reaches the user only from
	// the tree — Status.Resources carries no pod health.
	if tree != nil {
		if problems := listProblemNodes(tree); len(problems) > 0 {
			sections = append(sections, "Unhealthy resources:\n\t"+strings.Join(problems, "\n\t"))
		}
	}

	if resources := listOutOfSyncResources(app); len(resources) > 0 {
		sections = append(sections, "Out-of-sync resources:\n\t"+strings.Join(resources, "\n\t"))
	}

	if failed := listFailedSyncResultResources(app); len(failed) > 0 {
		sections = append(sections, "Failed resources:\n\t"+strings.Join(failed, "\n\t"))
	}

	return strings.Join(append(sections, lastSyncOperationLine(app)), "\n\n")
}

// buildDriftExplanation says why an app whose last sync succeeded is still out of sync, comparing
// the revision that sync applied with the one the running comparison uses: different means the
// desired state moved, same means applying it did not converge. Empty when ArgoCD reported no
// revisions or the sync failed. It never claims which revision is newer: rollbacks reverse that.
func buildDriftExplanation(app *models.Application) string {
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

	return explanation + "\n" + autoSyncNote(app)
}

// autoSyncNote states whether ArgoCD will act on the drift by itself, which decides whether the
// operator has to. It accompanies the drift explanation rather than standing alone: on its own it
// would add a line to every report that has nothing to explain.
func autoSyncNote(app *models.Application) string {
	if !autoSyncEnabled(app) {
		return "Auto-sync is disabled: ArgoCD applies the desired state only when a sync is triggered."
	}

	automated := app.Spec.SyncPolicy.Automated
	return fmt.Sprintf("Auto-sync is enabled (prune %s, self-heal %s).",
		onOff(automated.Prune), onOff(automated.SelfHeal))
}

// lastSyncOperationLine reports the outcome of the sync ArgoCD last ran. Unconditional, so a
// report whose every other section emptied out still says something.
func lastSyncOperationLine(app *models.Application) string {
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

// autoSyncEnabled reports whether ArgoCD applies this application's desired state on its own.
func autoSyncEnabled(app *models.Application) bool {
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

// isTerminalFailurePhase reports whether an ArgoCD phase is a terminal failure. It serves both
// OperationState.Phase and SyncResult.Resources[].HookPhase, separate upstream enums sharing one
// value set. "Running", "Succeeded", "Terminating" and "" are deliberately excluded.
func isTerminalFailurePhase(phase string) bool {
	return phase == "Failed" || phase == "Error"
}

// isProblemHealthStatus reports whether a resource HealthStatusCode is worth surfacing in the
// failure report. "Healthy" and "Progressing" would dilute the signal; "Synced" appears in legacy
// fixtures and is not actionable.
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
func listFailedSyncResultResources(app *models.Application) []string {
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

// listProblemResources returns one formatted line per resource whose health is a problem.
func listProblemResources(app *models.Application) []string {
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
func formatTreeNode(n models.ApplicationTreeNode) string {
	return withMessage(fmt.Sprintf("%s(%s) %s", n.Kind, n.Name, n.Health.Status), n.Health.Message)
}

// listProblemNodes returns formatted lines for resource-tree nodes whose health indicates a
// problem. This is where pod-level failure causes (ImagePullBackOff, CrashLoopBackOff) surface —
// they are carried by the Pod nodes, which never appear in models.Application.Status.Resources.
func listProblemNodes(tree *models.ApplicationTree) []string {
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

// listProgressingNodes returns formatted lines for resource-tree nodes still Progressing — the
// resources a rollout that ran out its timeout was still waiting on.
func listProgressingNodes(tree *models.ApplicationTree) []string {
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

// problemResourceLines returns the "Unhealthy resources" lines, preferring the live tree, which
// alone carries pod-level causes. Without a tree it falls back to Status.Resources only when
// allowStatusFallback is set: the not-available path has no resource listing yet, while the
// not-healthy path already lists those resources in its "Resources:" block.
func problemResourceLines(app *models.Application, tree *models.ApplicationTree, allowStatusFallback bool) []string {
	if tree != nil {
		return listProblemNodes(tree)
	}
	if allowStatusFallback {
		return listProblemResources(app)
	}
	return nil
}

// buildFailureDiagnostics builds the diagnostics suffix for the "not available" and
// "not healthy"/"degraded" failures. Sections are included only when they have content; with none
// the result is "", keeping the legacy output. tree is optional (see rolloutMessage) and
// allowStatusFallback controls the tree-less "Unhealthy resources" fallback.
func buildFailureDiagnostics(app *models.Application, tree *models.ApplicationTree, allowStatusFallback bool) string {
	var sections []string

	if isTerminalFailurePhase(app.Status.OperationState.Phase) {
		opSection := fmt.Sprintf("Sync operation phase: %s", app.Status.OperationState.Phase)
		if msg := app.Status.OperationState.Message; msg != "" {
			opSection += "\nSync operation message: " + msg
		}
		sections = append(sections, opSection)
	}

	if failed := listFailedSyncResultResources(app); len(failed) > 0 {
		sections = append(sections, "Failed resources:\n\t"+strings.Join(failed, "\n\t"))
	}

	if resources := problemResourceLines(app, tree, allowStatusFallback); len(resources) > 0 {
		sections = append(sections, "Unhealthy resources:\n\t"+strings.Join(resources, "\n\t"))
	}

	// A still-Progressing app that ran out its timeout has no problem node, since
	// isProblemHealthStatus excludes Progressing, so the failure would name nothing. Gated on the
	// app's own health, not on the absence of problem nodes: a Suspended CronJob or Missing resource
	// is a problem node but not the stall, and must not hide the workload that never became ready.
	if tree != nil && app.Status.Health.Status == "Progressing" {
		if pending := listProgressingNodes(tree); len(pending) > 0 {
			sections = append(sections, "Resources still progressing:\n\t"+strings.Join(pending, "\n\t"))
		}
	}

	return strings.Join(sections, "\n\n")
}
