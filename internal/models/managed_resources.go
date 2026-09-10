package models

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
// from the application source. TargetState is the string "null" for a resource that
// exists only in the cluster.
type ManagedResource struct {
	TargetState string `json:"targetState"`
}
