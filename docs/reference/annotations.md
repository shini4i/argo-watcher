# Argo Application Annotations

Argo Watcher uses Kubernetes annotations on Argo CD Application objects to configure per-application behavior.

All annotations are prefixed with `argo-watcher/` and are optional unless otherwise noted.

## Annotation Reference

| Annotation | Type | Scope | Default | Example | Description |
|---|---|---|---|---|---|
| `argo-watcher/images` | comma-separated string | Application | (none) | `app-service,database` | List of image names to monitor for updates. |
| `argo-watcher/image-tag` | string | Application | (none) | `latest` | Expected image tag to watch for. |
| `argo-watcher/deployment-locking` | string | Application | (none) | `on-schedule` | Enable deployment locking with a schedule. |

## Usage in Guides

For detailed examples of how to use annotations in your deployments, see:
- [GitOps Updater Guide](../guides/gitops-updater.md)
- [Deployment Locking](../guides/gitops-updater.md#deployment-locking)
