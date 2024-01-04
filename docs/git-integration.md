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

### Additional information

- The `app` alias is intended to correspond with an alias specified in the `argo-watcher/ALIAS.helm.image-tag` annotation.
- When processing annotations, Argo-Watcher will associate an image (e.g., `registry.example.com/group-name/project-name`) with all aliases that share this particular image. Consequently, you won't be able to implement different release strategies for aliases that utilize the same image name.

## CI/CD side configuration

In general, the example from the [installation](installation.md) page should be sufficient to get you started. However, there is one more things to consider.

You need to add the following environment variables to your CI/CD pipeline:

```
ARGO_WATCHER_DEPLOY_TOKEN=previously_generated_token
```

That's it! Starting from this point, Argo-Watcher will be able to commit changes to your GitOps repository.

> Keep in mind, that `argo-watcher` will use the provided tag value as is, without any validation. So, it is up to user to make sure that the tag is valid and can be used for deployment.
