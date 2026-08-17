# Annotations

Argo Watcher reads these annotations from the Argo CD `Application` resource. All are optional; see the [GitOps Updater guide](../guides/gitops-updater.md) for how they fit together.

| Annotation | Example | Description |
|---|---|---|
| `argo-watcher/managed` | `"true"` | Enables Argo Watcher management, and with it the GitOps write-back. |
| `argo-watcher/managed-images` | `app=registry.example.com/group/project` | Maps an alias to a full image name; comma-separated for several. |
| `argo-watcher/<alias>.helm.image-tag` | `app.image.tag` | Helm value path the new tag is written to, keyed by an alias from `managed-images`. |
| `argo-watcher/write-back-filename` | `values-override.yaml` | Overrides the override-file name (derived from the app name by default). |
| `argo-watcher/write-back-repo` | `git@github.com:example/gitops.git` | Write-back repository. **Multi-source applications only.** |
| `argo-watcher/write-back-branch` | `main` | Write-back branch. **Multi-source applications only.** |
| `argo-watcher/write-back-path` | `sandbox/charts/demo` | Write-back path. **Multi-source applications only.** |
| `argo-watcher/fire-and-forget` | `"true"` | Commits the tag and marks the task `deployed` without monitoring the rollout. |
| `argo-watcher/skip-image-validation` | `"true"` | Turns off the [fail-fast image check](../operations/troubleshooting.md#image-is-not-part-of-application), so deployments wait for the timeout instead. |

!!! warning
    The three `write-back-*` location annotations are honored only when the application uses `spec.sources` (plural). On a single-source application they are silently ignored — the location comes from the application's own source.
