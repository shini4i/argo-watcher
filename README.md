<div align="center">

# Argo Watcher
Improve visibility of deployments managed by Argo CD Image Updater

![GitHub Actions](https://img.shields.io/github/actions/workflow/status/shini4i/argo-watcher/run-tests-and-sonar-scan.yml?branch=main)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
[![codecov](https://codecov.io/gh/shini4i/argo-watcher/graph/badge.svg?token=9JI19X0BIN)](https://codecov.io/gh/shini4i/argo-watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shini4i/argo-watcher)](https://goreportcard.com/report/github.com/shini4i/argo-watcher)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/demo.png" alt="Showcase" height="441" width="620">
</div>

## Why use Argo Watcher

Argo Watcher solves an essential problem of visibility when deploying projects with Argo CD Image Updater.

Argo Watcher monitors ArgoCD API for changes in your application and syncs the status for your image-related changes.

## Prerequisites

Argo Watcher is a standalone application, for it is designed to work with:

1. ArgoCD
2. Argo CD Image Updater
3. CI/CD solution of your choice

## Possible workflow

The workflow for deployment might be the following
1. Build and push a new image of your application
2. The next step should be a job that runs Argo Watcher Client, which triggers deployment monitoring
3. Argo CD Image Updater detects a new image and commits the changes to GitOps repo that starts the deployment
4. Throughout the deployment, ArgoWatcher monitors the current status of the Application
5. The client returns the exit code based on the resulting task status, marking the pipeline as either successful or failed

<details>
<summary>A simplified diagram</summary>
<div align="center">

```mermaid
graph TD
    Dev[Dev] --> Commit{Commit changes to the git repository}
    Commit --> Pipeline[Build pipeline triggered]
    Pipeline --> Docker[Docker image build and published]
    Docker --> Task[Pipeline adds a task to Argo-Watcher]
    Task --> Check[Argo-Watcher checks Argo CD Api for an update]
    Check --> Decision{Is ArgoCD Application running on the expected image?}
    Decision -->|Yes| Success[Pipeline is marked as a success]
    Decision -->|No| Retry[Check API again]
    Retry --> TimeoutDecision{Has pre-defined timeout elapsed?}
    TimeoutDecision -->|Yes| Failed[Pipeline is marked as failed]
    TimeoutDecision -->|No| Check
```

</div>
</details>

## Documentation

> Starting with version v0.6.0, Argo Watcher now offers experimental support for making direct commits to the GitOps repository.

<details>
<summary>Direct Git Integration</summary>
If you've been using Argo CD Image Updater across hundreds of applications, you might have noticed that the latency in detecting new images can sometimes slow down your deployments considerably.

To address the challenges with deployment latency, we're excited to unveil an experimental feature in Argo Watcher that allows direct commits to your GitOps repository.

We remain committed to supporting the straightforward scenario where users simply check the Application status. This ensures flexibility for those who prefer or need to use the original method.

For those looking to experiment with faster image updates, you can leverage the new direct commit capability using the following annotations.

```yaml
  annotations:
    argo-watcher/managed: "true"
    argo-watcher/managed-images: "app=ghcr.io/shini4i/argo-watcher"
    argo-watcher/app.helm.image-tag: "image.tag"
```
This configuration will require mounting ssh key to the container. Support for this configuration is available in helm chart starting from verion `0.4.0`.

⚠️ Important Note Regarding Direct Commit Feature:

Please be aware that when using the direct commit feature, Argo Watcher does not verify the actual availability of the image. It assumes and trusts that the tag received from the client is correct. Ensure you have processes in place to validate image tags before relying on this feature.
</details>

- Installation instructions and more information can be found in the [docs](docs/installation.md).
- Development instructions can be found in the [docs](docs/development.md).
- A short story about why this project was created can be found [here](https://medium.com/dyninno/a-journey-to-gitops-9aa445474eb6).

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
