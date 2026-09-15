package models

// ApplicationManifests is the response of ArgoCD's /api/v1/applications/{name}/manifests
// endpoint: every manifest the application renders, each JSON-serialized. It is the only
// complete answer to "does this image belong to the application at all" — the desired state
// reported by the managed-resources endpoint omits every sync hook.
type ApplicationManifests struct {
	Manifests []string `json:"manifests"`
}
