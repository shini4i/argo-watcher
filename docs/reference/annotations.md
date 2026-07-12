# Argo Application Annotations

Argo Watcher uses Kubernetes annotations on Argo CD Application objects to configure per-application behavior.

All annotations are prefixed with `argo-watcher/` and are optional unless otherwise noted.

## Annotation Reference

| Annotation | Scope | Example | Description |
|---|---|---|---|
| `argo-watcher/managed` | Application | `true` | Enables Argo Watcher management (and the GitOps write-back) for this application. |
| `argo-watcher/managed-images` | Application | `app=registry.example.com/group/project` | Maps an alias to a full image name; comma-separated for multiple. |
| `argo-watcher/<alias>.helm.image-tag` | Application | `app.image.tag` | Helm value path the new tag is written to, keyed by the alias from `managed-images`. |
| `argo-watcher/write-back-repo` | Application (multi-source only) | `git@github.com:example/gitops.git` | Overrides the write-back repo. Honored only when the app uses `spec.sources` (plural). |
| `argo-watcher/write-back-branch` | Application (multi-source only) | `main` | Overrides the write-back branch (multi-source only). |
| `argo-watcher/write-back-path` | Application (multi-source only) | `sandbox/charts/demo` | Overrides the write-back path (multi-source only). |
| `argo-watcher/write-back-filename` | Application | `values-override.yaml` | Overrides the override-file name (default is derived from the app name). |
| `argo-watcher/fire-and-forget` | Application | `true` | Commits the tag and marks the deployment `deployed` without monitoring status. |

See the [Git Integration guide](../guides/gitops-updater.md) for full usage and examples.

## Usage in Guides

For detailed examples of how to use annotations in your deployments, see:
- [GitOps Updater Guide](../guides/gitops-updater.md)
- [Deployment Locking](../guides/gitops-updater.md#deployment-locking)
