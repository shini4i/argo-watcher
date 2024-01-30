---
hide:
- navigation
---
# argo-watcher

<figure markdown>
  ![Image title](https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/demo.png){ width="700" }
  <figcaption>Web UI example</figcaption>
</figure>

## General information

Argo Watcher not only addresses the critical challenge of visibility during deployments with Argo CD Image Updater but also introduces experimental support for direct commits to the GitOps repository.

It actively monitors the ArgoCD API for application changes and synchronizes the status of your image-related modifications, streamlining and potentially accelerating your deployment processes.

## Potential use cases

A simplified diagram of the possible workflow using ArgoCD Image Updater:

```mermaid
graph LR
    Dev[Dev] --> Commit{Commit}
    Commit --> Pipeline[Pipeline Triggered]
    Pipeline --> Docker[Image Built]
    Docker --> Task[Task added to Argo-Watcher]
    Task --> Check[Check Argo CD Api]
    Check --> Decision{Expected Image?}
    Decision -->|Yes| Success[Success]
    Decision -->|No| Retry[Retry API Check]
    Retry --> Timeout{Timeout?}
    Timeout -->|Yes| Failed[Failed]
    Timeout -->|No| Check
```

or a workflow using direct commits:

```mermaid
graph LR
    Dev[Dev] --> Commit{Commit}
    Commit --> Pipeline[Pipeline Triggered]
    Pipeline --> Docker[Image Built]
    Docker --> Task[Task added to Argo-Watcher]
    Task --> ImageUpdate[Commit to GitOps repo]
    ImageUpdate --> Check[Check Argo CD Api]
    Check --> Decision{Expected Image?}
    Decision -->|Yes| Success[Success]
    Decision -->|No| Retry[Retry API Check]
    Retry --> Timeout{Timeout?}
    Timeout -->|Yes| Failed[Failed]
    Timeout -->|No| Check
```
