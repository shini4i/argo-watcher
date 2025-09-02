<div align="center">

# Argo Watcher

**A feedback loop for your GitOps workflow.**

Argo Watcher bridges the gap between your CI pipeline and Argo CD, providing real-time status and visibility into your deployments. No more "fire-and-forget" deployments.

![GitHub Actions](https://img.shields.io/github/actions/workflow/status/shini4i/argo-watcher/run-tests-and-sonar-scan.yml?branch=main)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
[![codecov](https://codecov.io/gh/shini4i/argo-watcher/graph/badge.svg?token=9JI19X0BIN)](https://codecov.io/gh/shini4i/argo-watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shini4i/argo-watcher)](https://goreportcard.com/report/github.com/shini4i/argo-watcher)
[![Documentation Status](https://readthedocs.org/projects/argo-watcher/badge/?version=latest)](https://argo-watcher.readthedocs.io/en/latest/?badge=latest)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/demo.png" alt="Argo Watcher UI" height="441" width="680">

</div>

## The Problem

In a typical GitOps workflow, a CI pipeline builds an image, pushes it to a registry, and updates a Git repository. Argo CD then detects the change and deploys the new image. The problem is that the CI pipeline has no direct knowledge of the deployment's outcome. Did it succeed? Did it fail? The pipeline is left in the dark.

## The Solution

Argo Watcher introduces a control loop that monitors your Argo CD applications for health and sync status changes. It acts as a bridge, reporting the deployment's final state back to the CI pipeline. This provides a clear, synchronous result for an asynchronous process.

## Key Features

*   **Deployment Tracking**: Monitors Argo CD applications and reports on their health and sync status.
*   **CI Integration**: A lightweight client that can be integrated into any CI/CD pipeline to wait for a successful deployment.
*   **Real-time Web UI**: A comprehensive dashboard to visualize deployment status, history, and application state.
*   **Built-in GitOps Updater**: An optional, standalone service to update image tags in your GitOps repository, as an alternative to the Argo CD Image Updater.
*   **Notifications**: Send deployment status notifications to webhooks.
*   **Authentication**: Supports JWT and Keycloak for secure access to the server and UI.

## Architecture

Argo Watcher consists of three main components: the **Server**, the **Client**, and the **Web UI**.

```mermaid
graph TD
    subgraph "Your Environment"
        CI_Pipeline[CI Pipeline]
        Git_Repo[GitOps Repository]
        Image_Registry[Image Registry]
    end

    subgraph "Argo Watcher"
        Watcher_Server[Server]
        Watcher_Client[Client]
        Watcher_WebUI[Web UI]
        Watcher_Updater[GitOps Updater]
    end

    subgraph "Argo CD"
        ArgoCD_API[Argo CD API]
        ArgoCD_UI[Argo CD UI]
    end

    CI_Pipeline -- "1. Build & Push" --> Image_Registry
    CI_Pipeline -- "2. Run" --> Watcher_Client
    Watcher_Client -- "3. Create Task" --> Watcher_Server
    Watcher_Server -- "4. (Optional) Update Image Tag" --> Watcher_Updater
    Watcher_Updater -- "5. Commit" --> Git_Repo
    Watcher_Server -- "7. Monitor" --> ArgoCD_API
    ArgoCD_UI -- "6. Sync" --> Git_Repo
    Watcher_Server -- "8. Stream Status" --> Watcher_WebUI
    Watcher_Server -- "8. Report Status" --> Watcher_Client
    Watcher_Client -- "9. Exit Code" --> CI_Pipeline
```

## How It Works

1.  **Trigger**: Your CI pipeline builds a new image and pushes it to a registry.
2.  **Monitor**: The pipeline then runs the Argo Watcher client, telling it which application and image to track.
3.  **Update**: The image tag is updated in your GitOps repository, either by the Argo CD Image Updater or Argo Watcher's built-in updater.
4.  **Deploy**: Argo CD detects the change and starts deploying the new image.
5.  **Track & Report**: The Argo Watcher server continuously polls the Argo CD API. As the deployment progresses, it streams status updates to the Web UI and reports the final status (e.g., `deployed`, `failed`) back to the client.
6.  **Complete**: The client exits with a status code that reflects the deployment outcome, allowing your CI pipeline to proceed or fail accordingly.

## Documentation

For more detailed information on configuration, API usage, and advanced features, please visit our documentation at [argo-watcher.readthedocs.io](https://argo-watcher.readthedocs.io).

## Contributing

Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
