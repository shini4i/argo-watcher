# Annotations

Argo Watcher reads these annotations from the Argo CD `Application` resource. All are optional; see the [GitOps Updater guide](../guides/gitops-updater.md) for how they fit together.

| Annotation | Example | Description |
|---|---|---|
| `argo-watcher/managed` | `"true"` | Enables Argo Watcher management, and with it the GitOps write-back. |
| `argo-watcher/managed-images` | `app=registry.example.com/group/project` | Maps an alias to a full image name; comma-separated for several. Whitespace around `=` is ignored. An entry with an empty alias or image, whitespace inside either, or a second `=` is rejected, as is a repeated alias. |
| `argo-watcher/write-back-mode` | `inline` | `override` (default) writes an override file. `inline` writes the tag into the marked fields of the `Application` manifest — see [Inline write-back](../guides/gitops-updater.md#inline-write-back). Any other value fails the write-back. |
| `argo-watcher/<alias>.helm.image-tag` | `app.image.tag` | Helm value path the new tag is written to, keyed by an alias from `managed-images`. **Override mode only.** |
| `argo-watcher/write-back-filename` | `values-override.yaml` | Override mode: the override-file name (derived from the app name by default). Inline mode: the manifest file holding the marked fields; required. |
| `argo-watcher/write-back-repo` | `git@github.com:example/gitops.git` | Write-back repository. **Multi-source applications or inline mode only**; required in inline mode. |
| `argo-watcher/write-back-branch` | `main` | Write-back branch. **Multi-source applications or inline mode only**; required in inline mode. |
| `argo-watcher/write-back-path` | `sandbox/charts/demo` | Write-back path. **Multi-source applications or inline mode only**; required in inline mode (`.` for the repository root). |
| `argo-watcher/fire-and-forget` | `"true"` | Commits the tag and marks the task `deployed` without monitoring the rollout. |
| `argo-watcher/skip-image-validation` | `"true"` | Turns off the [fail-fast image check](../operations/troubleshooting.md#image-is-not-part-of-application), so deployments wait for the timeout instead. |

!!! warning
    In override mode, the three `write-back-*` location annotations are honored only when the application uses `spec.sources` (plural). On a single-source application they are silently ignored — the location comes from the application's own source.
