# Git Integration

Argo Watcher includes a built-in GitOps updater that commits image tag changes directly to your GitOps repository. This eliminates the need for Argo CD Image Updater and removes the additional deployment latency it can introduce when managing a large number of images.

!!! tip
    If you are currently using Argo CD Image Updater and want to migrate, see the [Migrating from Argo CD Image Updater](#migrating-from-argo-cd-image-updater) section.

## Prerequisites

This guide assumes you already have a working Argo Watcher installation. If not, complete the [Installation](installation.md) guide first.

Before enabling the GitOps updater, you need to:

1. **Create an authentication secret** (choose one approach):
    - **Deploy token** -- Generate an arbitrary string and add it to the Argo Watcher Kubernetes secret under the `ARGO_WATCHER_DEPLOY_TOKEN` key. This approach is planned for deprecation in v1.0.0.
    - **JWT secret (recommended)** -- Generate a secret for signing JWT tokens and add it to the Argo Watcher Kubernetes secret under the `JWT_SECRET` key. See [JWT Configuration](#jwt-configuration) below.

2. **Create an SSH key secret** -- Generate an SSH key pair and store the private key in a Kubernetes secret. By default, Argo Watcher expects the key under the `sshPrivateKey` field, but this is configurable via the Helm chart.

3. **Update the Helm chart values** -- Add the updater configuration:

    ```yaml
    argo:
      updater:
        sshSecretName: "argo-watcher-ssh"
        commitAuthor: "Argo Watcher"
        commitEmail: "argo-watcher@example.com"
    ```

## JWT Configuration

JWT is the recommended authentication method for the GitOps updater. It provides fine-grained control over which applications a token can deploy to.

### JWT Payload Structure

```json
{
  "sub": "argo-watcher-client",
  "cluster": "prod",
  "allowed_apps": ["app1", "app2"],
  "iat": 1738692070,
  "exp": 1770228106
}
```

| Field          | Description                                                                 |
|----------------|-----------------------------------------------------------------------------|
| `sub`          | Token subject. Can be any value (e.g., service name or team identifier).   |
| `cluster`      | Cluster identifier. Can be any value.                                       |
| `allowed_apps` | List of Argo CD application names this token can deploy to. Use `"*"` to allow all applications. |
| `iat`          | Issued-at timestamp (Unix epoch). Use `date +%s` to generate.             |
| `exp`          | Expiration timestamp (Unix epoch). Set to a reasonable duration.           |

!!! note
    Application-level filtering based on `allowed_apps` is not yet implemented and is expected in a future release.

### Generating a JWT Token

You can use [jwt-cli](https://github.com/mike-engel/jwt-cli) to generate tokens:

```bash
jwt encode \
  --secret="YOUR_JWT_SECRET" \
  '{"sub":"argo-watcher-client","cluster":"prod","allowed_apps":["app1"],"iat":1738692070,"exp":1770228106}'
```

Replace `YOUR_JWT_SECRET` with the value stored in the `JWT_SECRET` key of your Kubernetes secret. Update the `iat` and `exp` timestamps as appropriate.

## Application Configuration

Argo Watcher uses Argo CD application annotations to determine how to manage image tag updates. All configuration is applied directly to the Argo CD Application resource.

### Required Annotations

```yaml
metadata:
  annotations:
    argo-watcher/managed: "true"
    argo-watcher/managed-images: "app=registry.example.com/group-name/project-name"
    argo-watcher/app.helm.image-tag: "app.image.tag"
```

**How annotations work:**

- `argo-watcher/managed` -- Enables Argo Watcher management for this application.
- `argo-watcher/managed-images` -- Maps an alias (`app`) to a full image name. The alias is used to reference the image in other annotations.
- `argo-watcher/ALIAS.helm.image-tag` -- Specifies the Helm value path where the image tag should be written. Replace `ALIAS` with the alias defined in `managed-images`.

!!! warning
    When processing annotations, Argo Watcher associates an image with all aliases that share the same image name. You cannot use different release strategies for aliases that use the same image.

### Multi-Source Applications

Argo CD supports applications with multiple sources. To use the GitOps updater with multi-source applications, add the following annotations:

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

For an application named `Demo`, Argo Watcher creates or updates the override file at:

```text
sandbox/charts/demo/.argocd-source-demo.yaml
```

!!! note
    The override file path is derived from the application name. This is the currently supported approach for reliably identifying the correct file to update.

### Fire-and-Forget Mode

In some cases, you may want to update the image tag without waiting for the deployment to complete. This is useful for applications that only contain `CronJob` resources, where the updated image won't be running immediately.

Add the following annotation to enable this mode:

```yaml
metadata:
  annotations:
    argo-watcher/fire-and-forget: "true"
```

When this annotation is set, Argo Watcher commits the image tag change and immediately marks the deployment as `deployed` without monitoring the application status.

### Custom Commit Messages

The default commit message format is:

```text
argo-watcher(appName): update image tag
```

You can customize the commit message using a Go template in the `COMMIT_MESSAGE_FORMAT` environment variable:

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

For available template variables, see the [Notifications](notifications.md#available-template-variables) page.

## CI/CD Configuration

After configuring the server and application annotations, update your CI/CD pipeline to provide the authentication token.

**Using a deploy token** (planned for deprecation):

```bash
export ARGO_WATCHER_DEPLOY_TOKEN=your_deploy_token
```

**Using JWT (recommended):**

```bash
export BEARER_TOKEN="Bearer your_jwt_token"
```

See the [Installation](installation.md#client-setup) guide for complete CI/CD pipeline examples.

!!! warning
    Argo Watcher uses the provided image tag value as-is, without validation. Ensure the tag is valid and corresponds to an image that exists in the registry.

## Deployment Locking

Argo Watcher supports a deployment lock mechanism to prevent changes during maintenance windows or other critical periods. When a lock is active, all new deployment tasks are rejected.

### Scheduled Lockdown

Define recurring maintenance windows using a schedule:

```yaml
extraEnvs:
  - name: LOCKDOWN_SCHEDULE
    value: "Wed 20:00 - Thu 08:00, Fri 20:00 - Mon 08:00"
```

Or use the Helm chart values:

```yaml
scheduledLockdown:
  - "Wed 20:00 - Thu 08:00"
  - "Fri 20:00 - Mon 08:00"
```

In this example, deployments are blocked between Wednesday 20:00 and Thursday 08:00, and between Friday 20:00 and Monday 08:00.

### Manual Lockdown

#### Via API

Set a lock:

```bash
curl -X POST https://argo-watcher.example.com/api/v1/deploy-lock
```

Release a lock:

```bash
curl -X DELETE https://argo-watcher.example.com/api/v1/deploy-lock
```

!!! note
    Direct API access to the deploy lock endpoints is only available when Keycloak integration is disabled. With Keycloak enabled, use the Web UI instead.

#### Via Web UI

Click the **Argo Watcher** logo in the Web UI and toggle the **Lockdown Mode** switch.

![Deployment Lock UI](https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/deployment-lock.png)

!!! note
    When Keycloak integration is enabled, you must be a member of a [privileged group](keycloak.md) to manage the deployment lock.

## Migrating from Argo CD Image Updater

If you are currently using Argo CD Image Updater, follow these steps to migrate an application to Argo Watcher:

**1. Replace the Image Updater annotations:**

Remove the existing Argo CD Image Updater annotations:

```yaml
# Remove these annotations
argocd-image-updater.argoproj.io/image-list: app=registry.example.com/group-name/project-name
argocd-image-updater.argoproj.io/app.update-strategy: latest
argocd-image-updater.argoproj.io/app.helm.image-name: app.image.repository
argocd-image-updater.argoproj.io/app.helm.image-tag: app.image.tag
argocd-image-updater.argoproj.io/app.allow-tags: regexp:^\d{7}-stage
```

**2. Add the Argo Watcher annotations:**

```yaml
# Add these annotations
argo-watcher/managed: "true"
argo-watcher/managed-images: "app=registry.example.com/group-name/project-name"
argo-watcher/app.helm.image-tag: "app.image.tag"
```

**3. Update your CI/CD pipeline** to include the Argo Watcher client step and authentication token (see [CI/CD Configuration](#cicd-configuration)).

**4. Test the migration** on a non-production application first, then roll out to the rest of your applications.
