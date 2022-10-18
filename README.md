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
2. Argo Image Updater
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

# Server Installation

Argo Watcher Server is designed to run in Kubernetes environment.

We provide a separate [helm chart](https://artifacthub.io/packages/helm/shini4i/argo-watcher) for deploying the server.

Helm chart values configurations example

```yaml
# credentials to access ArgoCD
argo:
  url: https://argocd.argocd.svc.cluster.local
  username: argo-watcher
  secretName: "argo-watcher"

# credentials to access postgresql and store deployment monitoring tasks
# can be omitted if persistence is not required (state will be stored in memory)
postgres:
  enabled: true
  host: argo-watcher-postgresql.argo-watcher-postgresql.svc.cluster.local
  name: argo-watcher
  user: argo-watcher
  secretName: "argo-watcher-postgresql"

# configurations to access Argo Watcher Server API and UI
ingress:
  enabled: true
  hosts:
    - host: argo-watcher.example.com
      paths:
        - path: /
          pathType: ImplementationSpecific
  tls:
    - secretName: tls-secret
      hosts:
        - argo-watcher.example.com
```

# Client Installation

The client is designed to run on Kubernetes runners. We have a [dedicated docker image](https://ghcr.io/shini4i/argo-watcher-client) for Argo Watcher Client CI/CD jobs.

## Running on GitLab CI/CD

Example deployment setup for running with GitLab CI/CD (reference: https://docs.gitlab.com/ee/ci/docker/using_kaniko.html)

```yaml
# we have only deployment stage
stages:
  - deploy

# build new image
build:
  stage: deploy
  image:
    name: gcr.io/kaniko-project/executor:v1.9.0-debug
    entrypoint: [""]
  script:
    - /kaniko/executor
      --context "${CI_PROJECT_DIR}"
      --dockerfile "${CI_PROJECT_DIR}/Dockerfile"
      --destination "${CI_REGISTRY_IMAGE}:${CI_COMMIT_TAG}"
  # build only on main
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

# deployment monitoring with Argo Watcher
watch:
  image: ghcr.io/shini4i/argo-watcher-client:v0.0.12
  variables:
    ARGO_WATCHER_URL: https://argo-watcher.example.com
    ARGO_APP: example
    COMMIT_AUTHOR: $GITLAB_USER_EMAIL
    PROJECT_NAME: $CI_PROJECT_PATH
    IMAGES: $CI_REGISTRY_IMAGE
    IMAGE_TAG: $CI_COMMIT_TAG
    DEBUG: 1
  script: ["/bin/client"]
  # run after we build the image
  needs: [ build ]
  # wait only on main and only when build was successfull
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
      when: success

```

Argo Watcher Client supports the following environment variables

| Variable | Description | Mandatory |
|---|---|---|
| ARGO_WATCHER_URL | The url of argo-watcher instance | Yes |
| ARGO_APP | The name of argo app to check for images rollout | Yes |
| COMMIT_AUTHOR | The person who made commit/triggered pipeline | Yes |
| PROJECT_NAME | An identificator of the business project (not related to argo project) | Yes |
| IMAGES | A list of images (separated by ",") that should contain specific tag | Yes |
| IMAGE_TAG | An image tag that is expected to be rolled out | Yes |
| DEBUG | Print various debug information | No |


# Development

## Prerequisites
This project depends on various git hooks. ([pre-commit](https://pre-commit.com))

They can be installed by running:
```bash
pre-commit install
```

## Back-End Development

To start developing argo-watcher you will need golang 1.19+

Start mock of the argo-cd server
```shell
# go to mock directory
cd cmd/mock
# start the server
go run .
```

Start the argo-watcher server
```shell
# go to backend directory
cd cmd/argo-watcher
# install dependencies
go mod tidy
# start argo-watcher
ARGO_URL=http://localhost:8081 STATE_TYPE=in-memory go run .
```

## Front-End Development

To start developing front-end you will need
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

## Requests examples

### Add a task
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

### Get task details
The ID provided in response for POST request should be provided to get task status:
```bash
curl http://localhost:8080/api/v1/tasks/be8c42c0-a645-11ec-8ea5-f2c4bb72758a
```
Example response:
```bash
{"status":"in progress"}
```

## Swagger
A swagger documentation can be accessed via http://localhost:8080/swagger/index.html
