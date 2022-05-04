# argo-watcher
![Sonar Quality Gate](https://img.shields.io/sonar/quality_gate/shini4i_argo-watcher?server=https%3A%2F%2Fsonarcloud.io)
![Sonar Coverage](https://img.shields.io/sonar/coverage/shini4i_argo-watcher?server=https%3A%2F%2Fsonarcloud.io)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/shini4i/argo-watcher)
![GitHub](https://img.shields.io/github/license/shini4i/argo-watcher)

## Project Description

This project is currently in PoC mode. The main idea is that this simple app will act as a "proxy" between pipelines and ArgoCD.

Currently, there is a limitation while using [argocd-image-updater](https://github.com/argoproj-labs/argocd-image-updater) that makes it hard
to understand what is happening after an image is built in a pipeline and before ArgoCD deploys the built image.

This app allows checking the status of built image deployment from the pipeline, which will help to
1) Have clear visibility of when the deployment is finished w/o checking ArgoCD.
2) Mark a pipeline as either successful or failed depending on the deployment result

Additionally, there is a Web UI that allows checking all current deployments and the history of old deployments.

### Expected workflow
1) argocd-image-updater is configured for auto-update of the required Application.
2) A pipeline is triggered
3) Docker image is packaged and tagged according to the pre-defined naming convention and pushed to the Docker registry.
4) The pipeline starts to communicate with argo-watcher and waits for either "deployed" or "failed" status to be returned.
5) It happens alongside with step 4. argocd-image-updater detects that the new image tag appeared in the registry and commits changes to the gitops repo.

## Prerequisites
This project depends on various git hooks. ([pre-commit](https://pre-commit.com))

They can be installed by running:
```bash
pre-commit install
```

## Development

There are 2 ways of how you can run argo-watcher locally
1. with docker compose
2. with locally installed python, poetry, nodejs, npm

### Docker Compose development

Create .env file with necessary environment variables

An overview on how to start docker compose setup
```shell
# install backend dependencies
docker compose run --rm backend poetry install
# install frontend dependencies
docker compose run --rm frontend npm install
# start database, backend and frontend
docker compose --profile complete up
# or you can start only the database
docker compose up
```

Backend container needs to be restarted manually
```shell
docker restart argo-watcher-backend-1
```

> Frontend container monitors file changes and reloads server automatically.

### Back-End Development

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

### Front-End Development

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

### Requests examples
> OpenAPI endpoint is available under http://localhost:8080/docs endpoint
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
