---
hide:
- navigation
---
# git-integration

## General information

Argo-Watcher was originally designed to complement ArgoCD Image Updater. Its primary purpose was to address the critical issue of insufficient visibility regarding the transition of Docker images from the build phase to their deployment in the live environment.

Over time, ArgoCD Image Updater started to impede deployment efficiency, primarily due to the management of a large number of images. This resulted in an additional 3-15 minutes of deployment time.

After careful deliberation, we made the decision to expand the functionality of Argo-Watcher. This expansion allows Argo-Watcher to commit changes with image tag overrides, effectively circumventing the deployment bottleneck entirely.

## Prerequisites

We assume you already have a working instance of Argo-Watcher and want to extend its functionality. For instructions regarding initial installation please check the [installation](installation.md) page.

Before moving to the actual configuration, you need to:

1. Generate a token that would be used to validate requests from GitLab/Github. It can be any string. (it should be added to the secret used by argo-watcher under the `ARGO_WATCHER_DEPLOY_TOKEN` key)
2. Create a secret with ssh key that will be used by `argo-watcher` to make commits to the GitOps repository. (by default, we expect it to be available under the `sshPrivateKey`, but can be configured via helm chart values)
3. Bump chart version to > `0.4.3` to support the necessary configuration

The following configuration should be added to the `argo-watcher` helm chart values (adjust according to your needs):

```yaml
argo:
  updater:
    sshSecretName: "argo-watcher-ssh"
    commitAuthor: "Argo Watcher"
    commitEmail: "argo-watcher@example.com"
```

## Application side configuration

Argo-Watcher boasts a more straightforward logic, which, in turn, simplifies the configuration required to enable its functionality.

Configuration is carried out similarly to ArgoCD Image Updater, with all settings conveyed through application annotations.


To migrate the project to Argo-Watcher management, we need to adjust the following annotations:

```yaml
argocd-image-updater.argoproj.io/image-list: app=registry.example.com/group-name/project-name

argocd-image-updater.argoproj.io/app.update-strategy: latest
argocd-image-updater.argoproj.io/app.helm.image-name: app.image.repository
argocd-image-updater.argoproj.io/app.helm.image-tag: app.image.tag
argocd-image-updater.argoproj.io/app.allow-tags: regexp:^\d{7}-stage
```

to the following:

```yaml
argo-watcher/managed: "true"
argo-watcher/managed-images: "app=registry.example.com/group-name/project-name"
argo-watcher/app.helm.image-tag: "app.image.tag"
```

### Multi source Applications

As ArgoCD now supports an array of sources, Argo-Watcher can also work with them. But in less dynamic way. You need to add more annotations to the application:

```yaml
argo-watcher/write-back-repo: "git@github.com:example/gitops.git"
argo-watcher/write-back-branch: "main"
argo-watcher/write-back-path: "sandbox/charts/demo"
```

Assuming that our application name is `Demo`, argo-watcher will create/update the following override file:
```bash
sandbox/charts/demo/.argocd-source-demo.yaml
```

> This is not an ideal solution, but so far it is the only way to reliably determine the correct override file to update.

### Additional information

- The `app` alias is intended to correspond with an alias specified in the `argo-watcher/ALIAS.helm.image-tag` annotation.
- When processing annotations, Argo-Watcher will associate an image (e.g., `registry.example.com/group-name/project-name`) with all aliases that share this particular image. Consequently, you won't be able to implement different release strategies for aliases that utilize the same image name.

### Customize commit message

The default commit message is in the following format:

`argo-watcher(appName): update image tag`

Commit message can be customized by setting the following environment variable:

```yaml
extraEnvs:
  - name: COMMIT_MESSAGE_FORMAT
    value: 'argo-watcher({{.App}}): update image tag\nID: {{.Id}}\nAuthor: {{.Author}}\nImages:\n{{range .Images}}{{.Image}}:{{.Tag}}\n{{end}}'
```
Commit message supports templated variables. For available template variables see [notifications](notifications.md#available-template-variables) page.

## CI/CD side configuration

In general, the example from the [installation](installation.md) page should be sufficient to get you started. However, there is one more things to consider.

You need to add the following environment variables to your CI/CD pipeline:

```
ARGO_WATCHER_DEPLOY_TOKEN=previously_generated_token
```

That's it! Starting from this point, Argo-Watcher will be able to commit changes to your GitOps repository.

> Keep in mind, that `argo-watcher` will use the provided tag value as is, without any validation. So, it is up to user to make sure that the tag is valid and can be used for deployment.

## Lockdown mode

Argo-Watcher supports a deployment lock mechanism. It can be useful when you want to prevent Argo-Watcher from making changes during the maintenance window.

There are two ways to enable the deployment lock:

### Scheduled Lockdown mode

In order to create a scheduled Lockdown mode, you need to set the following environment variables:

```yaml
extraEnvs:
  - name: LOCKDOWN_SCHEDULE
    value: 'Wed 20:00 - Thu 08:00, Fri 20:00 - Mon 08:00'
```

In this example, the deployments won't be allowed between Wednesday 20:00 and Thursday 08:00, and between Friday 20:00 and Monday 08:00.

### Manual Lockdown mode

#### CLI

In order to set Lockdown mode manually, you need to make POST request:

```bash
curl -X POST https://argo-watcher.example.com/api/v1/deploy-lock
```

and to remove it make DELETE request:

```bash
curl -X DELETE https://argo-watcher.example.com/api/v1/deploy-lock
```

> Keep in mind that it will work only if keycloak integration is not enabled.

#### Frontend

You can set Lockdown mode manually via the frontend. To do so, click on `Argo-Watcher` logo and press on `Lockdown Mode` switch.

![Image title](https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/deployment-lock.png)

> If you have keycloak integration enabled, you need to be a member of one of pre-defined privileged groups to be able to set deployment lock.
