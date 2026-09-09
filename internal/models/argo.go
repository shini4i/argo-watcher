package models

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

// IsManagedByWatcher reports whether the app carries the "argo-watcher/managed=true" annotation.
func (app *Application) IsManagedByWatcher() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[managedAnnotation] == "true"
}

// IsFireAndForgetModeActive reports whether the app carries "argo-watcher/fire-and-forget=true".
func (app *Application) IsFireAndForgetModeActive() bool {
	if app.Metadata.Annotations == nil {
		return false
	}
	return app.Metadata.Annotations[fireAndForgetAnnotation] == "true"
}

// IsImageValidationSkipped reports whether the app carries the skip-image-validation annotation.
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
