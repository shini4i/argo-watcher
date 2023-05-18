<div align="center">

# Argo Watcher
Improve visibility of deployments managed by Argo CD Image Updater

![GitHub Actions](https://img.shields.io/github/actions/workflow/status/shini4i/argo-watcher/run-tests-and-sonar-scan.yml?branch=main)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
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
<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/simplified_diagram.png" alt="Showcase" height="540" width="540">
</div>
</details>

## Documentation

- Installation instructions and more information can be found in the [docs](docs/installation.md).
- Development instructions can be found in the [docs](docs/development.md).
- A short story about why this project was created can be found [here](https://medium.com/dyninno/a-journey-to-gitops-9aa445474eb6).

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
