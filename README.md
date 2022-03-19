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
