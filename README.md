<div align="center">

# Argo Watcher
The project bridges traditional pipelines and GitOps, improving deployment visibility with Argo CD Image Updater and a built-in GitOps repo updater

![GitHub Actions](https://img.shields.io/github/actions/workflow/status/shini4i/argo-watcher/run-tests-and-sonar-scan.yml?branch=main)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
[![codecov](https://codecov.io/gh/shini4i/argo-watcher/graph/badge.svg?token=9JI19X0BIN)](https://codecov.io/gh/shini4i/argo-watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shini4i/argo-watcher)](https://goreportcard.com/report/github.com/shini4i/argo-watcher)
[![Documentation Status](https://readthedocs.org/projects/argo-watcher/badge/?version=latest)](https://argo-watcher.readthedocs.io/en/latest/?badge=latest)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/demo.png" alt="Showcase" height="441" width="680">

</div>

## Why Use Argo Watcher

Argo Watcher not only addresses the critical challenge of visibility during deployments with Argo CD Image Updater but also introduces optional built-in image updater.

It actively monitors the ArgoCD API for application changes and synchronizes the status of your image-related modifications, streamlining and potentially accelerating your deployment processes.

## Prerequisites

1. ArgoCD
2. Argo CD Image Updater (we encourage you to try out built-in GitOps repo updater instead)
3. CI/CD solution of your choice

## Possible workflow

A possible workflow with Argo Watcher:

1. **Build and Deploy**: Build a new Docker image of your application and push it to your image repository.
2. **Monitoring Setup**: Run an Argo Watcher Client job after the new image is pushed. This job oversees the deployment process.
3. **Image Update in GitOps repo**: Argo CD Image Updater or Argo Watcher commits the updated image tag to the GitOps repository, triggering deployment.
4. **Deployment Monitoring**: Argo Watcher monitors and reports the deployment status in real-time.
5. **Pipeline Status Reporting**: The client returns an exit code reflecting the deployment task status, marking the workflow's completion.

<div align="center">
<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/simplified_diagram.png" alt="Showcase" height="540" width="540">
</div>

> [!TIP]
> In addition to pipeline logs, the whole process can be observed through the web UI.

## Documentation

The up to date documentation is available here: [argo-watcher.readthedocs.io](https://argo-watcher.readthedocs.io).

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
