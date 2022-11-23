<div align="center">

# Argo Watcher
Improve visibility of ArgoCD Image Updater deployments

![GitHub Actions](https://img.shields.io/github/workflow/status/shini4i/argo-watcher/Build%20and%20Publish%20docker%20images)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shini4i/argo-watcher)](https://goreportcard.com/report/github.com/shini4i/argo-watcher)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

<img src="https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/demo.png" alt="Showcase" height="441" width="620">
</div>

# Why use Argo Watcher

Argo Watcher solves an essential problem of visibility when deploying projects with Argo Image Updater.

Argo Watcher monitors ArgoCD API for changes in your application and syncs the status for your image-related changes.

# Prerequisites

Argo Watcher is a standalone application, for it is designed to work with
1. ArgoCD
2. Argo CD Image Updater
3. CI/CD of your choice

# How it works

First, you deploy Argo Watcher as a separate application.

Argo Watcher needs to know the URL of ArgoCD API and have the credentials to access it.

Deployment monitoring tasks are stored in the PostgreSQL database so Argo Watcher would need access to a database.


## Argo Watcher workflow

The workflow for deployment should be the following
1. Build a new image of your application in your CI/CD
2. The next step in CI/CD should be a job that runs Argo Watcher Client, which triggers deployment monitoring
3. Argo Image Updater detects a new image and starts the deployment
4. Throughout the deployment, the status of your deployment monitoring task changes in the Argo Watcher UI

## Author's commentary

Essentially we are trying to solve the following problems with the GitOps approach via ArgoCD:

1) Have clear visibility of when the deployment is finished w/o the user constantly checking ArgoCD UI
2) Mark a pipeline as either successful or failed depending on the deployment result

# Documentation

- Installation instructions and more information can be found in the [docs](docs/installation.md).
- Development instructions can be found in the [docs](docs/development.md).

# Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.