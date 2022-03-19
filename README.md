# argo-watcher
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=shini4i_argo-watcher&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=shini4i_argo-watcher)

PoC version of app that will watch if the required docker image was rolled out in ArgoCD.<br>
It is intended to be used in tandem with [argocd-image-updater](https://github.com/argoproj-labs/argocd-image-updater).

The reason for its existence is to provide clear visibility in pipelines that the specific code version is up and running w/o checking argocd ui.

## Expected workflow
1) argocd-image-updater is configured for auto-update of some specific Application.
2) Code build pipeline is triggered.
3) Docker image is packaged and tagged according to the pre-defined pattern and pushed to the Docker registry.
4) The pipeline starts to communicate with argo-watcher and waits for either "deployed" or "failed" status to be returned.
5) It happens alongside with step 4. argocd-image-updater detects that the new image tag appeared in the registry and commits changes to the gitops repo.

## Examples
### Add a task
Post request:
```bash
curl --header "Content-Type: application/json" \
     --request POST \
     --data '{"app":"test-app","author": "name", "images":[{"image":"example", "tag":"v1.8.0"}]}' \
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

# API Development

To start developing argo-watcher you will need
1. python version 3.10+
2. [poetry](https://python-poetry.org/) toolkit

Starting the API development

```shell
# create virtual environment
poetry shell
# install dependencies
poetry install
# start the project
poetry run
```

# Front-End Development

To start developing front-end you'd need
1. NodeJS version 16.6.0+
2. NPM (comes with NodeJS) 7.19.1+

```shell
# go into web directory
cd web
# install dependencies
npm install
# start web development server
npm start
```

The browser will open on http://localhost:3000
