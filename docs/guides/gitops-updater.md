# GitOps Updater

Argo Watcher can commit image-tag changes to your GitOps repository itself, replacing Argo CD Image Updater. The commit happens as part of the deployment, so there is no scan interval to wait through — which is what makes it faster when many images are in play.

Already on Argo CD Image Updater? See [Migrating from Argo CD Image Updater](#migrating-from-argo-cd-image-updater).

## Prerequisites

A working [installation](install.md), plus:

1. **A credential the client can present.** Either a **JWT** (recommended, see [JWT configuration](#jwt-configuration)) stored under the `JWT_SECRET` key of the Argo Watcher secret, or an arbitrary string under `ARGO_WATCHER_DEPLOY_TOKEN`. Without a valid credential the write-back is skipped, and the deployment fails blaming the image instead ([why](../operations/troubleshooting.md#image-tag-is-never-committed-write-back-skipped)).
2. **An SSH key with write access** to the GitOps repository, stored in a Kubernetes secret (the chart reads the `sshPrivateKey` key by default).
3. **Chart values pointing at that key:**

    ```yaml
    updater:
      sshSecretName: "argo-watcher-ssh"
      commitAuthor: "Argo Watcher"
      commitEmail: "argo-watcher@example.com"
    ```

    The chart also mounts a `ssh_known_hosts` file and points `SSH_KNOWN_HOSTS` at it. Host key verification is on, so a host missing from it fails the push — add yours via `updater.extraKnownHosts` or `updater.knownHostsConfigMap`.

## Application configuration

Argo Watcher reads annotations on the Argo CD `Application` resource. The minimum is:

```yaml
metadata:
  annotations:
    argo-watcher/managed: "true"
    argo-watcher/managed-images: "app=registry.example.com/group-name/project-name"
    argo-watcher/app.helm.image-tag: "app.image.tag"
```

- `managed` enables write-back for this application.
- `managed-images` maps an alias (`app`) to a full image name; comma-separate several.
- `<alias>.helm.image-tag` is the Helm value path the new tag is written to.

Every annotation is listed in the [annotations reference](../reference/annotations.md).

!!! warning
    An image is associated with **every** alias sharing its name, so two aliases pointing at the same image cannot use different release strategies.

### Multi-source applications

For an application built from `spec.sources` (plural), name the write-back target explicitly:

```yaml
metadata:
  annotations:
    argo-watcher/managed: "true"
    argo-watcher/managed-images: "app=registry.example.com/group-name/project-name"
    argo-watcher/app.helm.image-tag: "app.image.tag"
    argo-watcher/write-back-repo: "git@github.com:example/gitops.git"
    argo-watcher/write-back-branch: "main"
    argo-watcher/write-back-path: "sandbox/charts/demo"
```

!!! warning
    `write-back-repo`, `write-back-branch` and `write-back-path` are honored **only** for multi-source applications. On a single-`spec.source` application they are silently ignored — the repo, branch and path come from the application's own source, and only `write-back-filename` is still read.

The override file name is derived from the application name, so application `Demo` gets:

```text
sandbox/charts/demo/.argocd-source-demo.yaml
```

### Fire-and-forget mode

When the new image will not run on its own — an application containing only `CronJob` resources, for instance — annotate it:

```yaml
metadata:
  annotations:
    argo-watcher/fire-and-forget: "true"
```

Argo Watcher then commits the tag and marks the task `deployed` immediately, without monitoring the rollout.

### Custom commit messages

The default message is `argo-watcher(appName): update image tag`. Override it with a Go template in `COMMIT_MESSAGE_FORMAT`:

```yaml
extraEnvs:
  - name: COMMIT_MESSAGE_FORMAT
    value: >-
      argo-watcher({{.App}}): update image tag
      ID: {{.Id}}
      Author: {{.Author}}
      Images:
      {{range .Images}}{{.Image}}:{{.Tag}}
      {{end}}
```

The available fields are the same ones the [notification templates](notifications.md#template-variables) use.

## JWT configuration

JWT is the recommended credential: unlike the shared deploy token, each pipeline can hold its own, with an expiry.

### The signing secret

`JWT_SECRET` is a symmetric HMAC key you generate once and store in the Argo Watcher secret:

```bash
openssl rand -base64 32
```

!!! warning
    Anyone holding this value can mint deployment tokens. Keep it in the Kubernetes secret only, never in Git, and rotate it if it leaks.

### Claims

```json
{
  "sub": "argo-watcher-client",
  "cluster": "prod",
  "allowed_apps": ["app1", "app2"],
  "iat": 1773352800,
  "exp": 1804888800
}
```

| Claim | Validated | Notes |
|---|---|---|
| `exp` | Yes | **Required.** A token without it is rejected. |
| `iat` | Yes | A future `iat` is rejected. `date +%s` gives you the current value. |
| `nbf` | Yes | Optional; enforced by the JWT library. |
| `iss` | Only if `JWT_ISSUER` is set | Must equal it exactly. |
| `aud` | Only if `JWT_AUDIENCE` is set | A list matches when it contains the configured value. |
| `sub` | No | Informational — service or team name. |
| `cluster` | No | Informational. |
| `allowed_apps` | No | **Not enforced yet.** Per-application restriction is planned; today any valid token authorizes any application. |

### Binding a token to this server

`JWT_SECRET` alone says nothing about *who* minted a token. Where the same secret is reused across a CI estate, another system's tokens are valid deploy credentials here. Set `JWT_ISSUER`, `JWT_AUDIENCE`, or both on the server to require that a token names this deployment.

Both are unchecked while unset, so an existing fleet is unaffected. Once set they are strict — a token that omits the claim is rejected, not passed — which fixes the rollout order:

1. Update every pipeline to mint tokens carrying the claim.
2. Wait until no token minted without it is still in use (they last until their `exp`).
3. Set the variable on the server.

Doing it the other way round rejects every token in flight at once.

### Minting a token

With [jwt-cli](https://github.com/mike-engel/jwt-cli):

```bash
jwt encode \
  --secret="YOUR_JWT_SECRET" \
  '{"sub":"argo-watcher-client","cluster":"prod","allowed_apps":["app1"],"iat":1773352800,"exp":1804888800}'
```

Add `"iss"` and `"aud"` to the payload when the server is configured to require them.

## Pipeline configuration

Pass the credential to the client:

```bash
# JWT (recommended). The raw token, with no "Bearer " prefix, so CI can mask it.
export BEARER_TOKEN="your_jwt_token"

# Or the deploy token
export ARGO_WATCHER_DEPLOY_TOKEN=your_deploy_token
```

The [installation guide](install.md#run-the-client-in-ci) has complete pipeline examples.

!!! warning
    The image tag is used as given, without validation. A tag that does not exist in the registry is committed anyway, and the deployment then fails on the pull.

## Batch write-back

By default each application's write-back takes a per-repository lock and clones, commits, and pushes on its own. When many applications deploy to the **same** repository at once, that serialized tail can reach minutes.

Batch mode coalesces write-backs to one repository into a single clone and push:

```yaml
extraEnvs:
  - name: GIT_BATCH_WRITEBACK
    value: "true"
  - name: GIT_BATCH_MAX_SIZE   # optional, default 20
    value: "20"
```

Batching is contention-driven, so it costs nothing when there is no contention: a write-back for an idle repository flushes immediately, while ones arriving during an in-flight flush are queued and flushed together as soon as it completes. Each flush makes **one commit per application** — preserving per-app history and `COMMIT_MESSAGE_FORMAT` — followed by a single push. `GIT_BATCH_MAX_SIZE` bounds one flush; the rest carries into the next.

Correctness is unchanged. Each application writes its own `.argocd-source-<app>.yaml`, so applications in a batch cannot conflict; a misconfigured application fails alone; a superseded deployment is dropped from the batch rather than committing a stale tag; and a push that loses a race re-clones onto the new tip and re-applies, exactly as the serialized path does. `GIT_OP_TIMEOUT` and `GIT_MAX_ATTEMPTS` apply to the batch as a whole. With multiple replicas each coalesces its own write-backs, and the Postgres advisory lock still serializes pushes between them.

Watch `gitops_batch_size` to see whether batching is doing anything: a distribution clustered at `1` means there is no contention to collapse.

!!! warning
    A graceful shutdown stops retries at the next boundary but never interrupts the attempt in flight, and that attempt is bounded by `GIT_OP_TIMEOUT` (90s default) rather than the 25s shutdown budget. Keep `GIT_OP_TIMEOUT` under 25s if queued commits should land instead of being abandoned on restart. A clean drain still says nothing about the task's final **status** — see [Tasks stay "in progress" after a server restart](../operations/troubleshooting.md#tasks-stay-in-progress-after-a-server-restart).

## Migrating from Argo CD Image Updater

**1. Remove the Image Updater annotations:**

```yaml
argocd-image-updater.argoproj.io/image-list: app=registry.example.com/group-name/project-name
argocd-image-updater.argoproj.io/app.update-strategy: latest
argocd-image-updater.argoproj.io/app.helm.image-name: app.image.repository
argocd-image-updater.argoproj.io/app.helm.image-tag: app.image.tag
argocd-image-updater.argoproj.io/app.allow-tags: regexp:^\d{7}-stage
```

**2. Add the Argo Watcher ones:**

```yaml
argo-watcher/managed: "true"
argo-watcher/managed-images: "app=registry.example.com/group-name/project-name"
argo-watcher/app.helm.image-tag: "app.image.tag"
```

**3. Add the client step** and its credential to the pipeline (see [Pipeline configuration](#pipeline-configuration)).

**4. Test on a non-production application** before rolling out to the rest.
