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
</div>

## Why use Argo Watcher

Argo Watcher not only addresses the critical challenge of visibility during deployments with Argo CD Image Updater but also introduces experimental support for direct commits to the GitOps repository.

It actively monitors the ArgoCD API for application changes and synchronizes the status of your image-related modifications, streamlining and potentially accelerating your deployment processes.

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
    Dev[Dev] --> Commit{Commit}
    Commit --> Pipeline[Pipeline Triggered]
    Pipeline --> Docker[Image Built]
    Docker --> Task[Task to Argo-Watcher]
    Task --> Check[Check Argo CD Api]
    Check --> Decision{Expected Image?}
    Decision -->|Yes| Success[Success]
    Decision -->|No| Retry[Retry API Check]
    Retry --> Timeout{Timeout?}
    Timeout -->|Yes| Failed[Failed]
    Timeout -->|No| Check
```

</div>
</details>

## Documentation

The up to date documentation is available on the [readthedocs.io](https://argo-watcher.readthedocs.io).

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
