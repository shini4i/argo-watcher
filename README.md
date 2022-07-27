<div align="center">

# argo-watcher
A small tool that will help to improve deployment visibility

![GitHub Actions](https://img.shields.io/github/workflow/status/shini4i/argo-watcher/Build%20and%20Publish%20docker%20images)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/shini4i/argo-watcher)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shini4i/argo-watcher)](https://goreportcard.com/report/github.com/shini4i/argo-watcher)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

![demo](https://i.ibb.co/x3v7t1n/demo.png)
</div>

## Project Description

This project is in an early development phase; hence some breaking changes might and should be expected.

The main idea is that this simple app will act as a "proxy" between pipelines and ArgoCD.

Currently, there is a limitation while using [argocd-image-updater](https://github.com/argoproj-labs/argocd-image-updater) that makes it hard
to understand what is happening after an image is built in a pipeline and before ArgoCD deploys the built image.

This app allows checking the status of built image deployment from the pipeline, which will help to
1) Have clear visibility of when the deployment is finished w/o checking ArgoCD
2) Mark a pipeline as either successful or failed depending on the deployment result

Additionally, there is a Web UI that allows checking all current deployments and the history of old deployments.

## How to install
### Server
A server-side part can be installed using the helm chart that is available  [here](https://artifacthub.io/packages/helm/shini4i/argo-watcher)
### Client
A simple example of client implementation that can be used in a pipeline is available [here](https://github.com/shini4i/argo-watcher/tree/main/cmd/client)

## Development

There are 2 ways of how you can run argo-watcher locally
1. with docker compose
2. with locally installed golang, nodejs, npm

### Prerequisites
This project depends on various git hooks. ([pre-commit](https://pre-commit.com))

They can be installed by running:
```bash
pre-commit install
```
### Back-End Development

To start developing argo-watcher you will need
1. golang 1.18+

### Front-End Development

To start developing front-end you'd need
1. NodeJS version 17.7.0+
2. NPM (comes with NodeJS) 8.9.0+

```shell
# go into web directory
cd web
# install dependencies
npm install
# start web development server
npm start
```

The browser will open on http://localhost:3000

### Requests examples
#### Add a task
Post request:
```bash
curl --header "Content-Type: application/json" \
     --request POST \
     --data '{"app":"test-app","author":"name","project":"example","images":[{"image":"example", "tag":"v1.8.0"}]}' \
     http://localhost:8080/api/v1/tasks
```
Example response:
```bash
{"status":"accepted","id":"be8c42c0-a645-11ec-8ea5-f2c4bb72758a"}
```
#### Get task details
The ID provided in response for POST request should be provided to get task status:
```bash
curl http://localhost:8080/api/v1/tasks/be8c42c0-a645-11ec-8ea5-f2c4bb72758a
```
Example response:
```bash
{"status":"in progress"}
```
