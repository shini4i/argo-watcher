<div align="center">

# Argo Watcher
Enhancing Deployment Visibility with Argo CD Image Updater & Direct GitOps Repository Commit Support

![GitHub Actions](https://img.shields.io/github/actions/workflow/status/shini4i/argo-watcher/run-tests-and-sonar-scan.yml?branch=main)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
[![codecov](https://codecov.io/gh/shini4i/argo-watcher/graph/badge.svg?token=9JI19X0BIN)](https://codecov.io/gh/shini4i/argo-watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shini4i/argo-watcher)](https://goreportcard.com/report/github.com/shini4i/argo-watcher)
[![Documentation Status](https://readthedocs.org/projects/argo-watcher/badge/?version=latest)](https://argo-watcher.readthedocs.io/en/latest/?badge=latest)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/demo.png" alt="Showcase" height="441" width="620">

**Figure 1:** Argo Watcher Web UI

</div>

## Why use Argo Watcher

Argo Watcher not only addresses the critical challenge of visibility during deployments with Argo CD Image Updater but also introduces experimental support for direct commits to the GitOps repository.

It actively monitors the ArgoCD API for application changes and synchronizes the status of your image-related modifications, streamlining and potentially accelerating your deployment processes.

## Prerequisites

Argo Watcher is a standalone application, for it is designed to work with:

1. ArgoCD (obviously)
2. Argo CD Image Updater (can be omitted if you're using direct commits feature)
3. CI/CD solution of your choice

## Possible workflow

This is just one the possible workflows that can be implemented with Argo Watcher.
1) **Build and Deploy**: Initiate by building a new Docker image of your application and pushing it to the designated image repository.
2) **Monitoring Setup**: Once the new image is pushed, execute a job that runs Argo Watcher Client. This step is crucial for triggering and overseeing the deployment process.
3) **Image Update and GitOps Integration**: Upon detection of the new image, Argo CD Image Updater either automatically commits the updated image tag to the GitOps repository, or this update is performed by argo-watcher itself. This action initiates the deployment.
4) **Deployment Monitoring**: Throughout the deployment process, Argo Watcher diligently monitors and reports the current status of the application, ensuring transparency and real-time tracking.
5) **Pipeline Status Reporting**: The client concludes the process by returning an exit code that reflects the status of the deployment task. This code is instrumental in determining the success or failure of the pipeline, thus marking the completion of the deployment workflow.

<div align="center">
<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/simplified_diagram.png" alt="Showcase" height="540" width="540">

**Figure 2:** This is a simplified diagram of the workflow described above.
</div>

> :warning: In addition to pipeline logs, the whole process can be observed through the web UI.

## Documentation

The up to date documentation is available here: [argo-watcher.readthedocs.io](https://argo-watcher.readthedocs.io).

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
