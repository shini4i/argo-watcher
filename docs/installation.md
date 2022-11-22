# Server Installation

Argo Watcher Server is designed to run in Kubernetes environment.

We provide a separate [helm chart](https://artifacthub.io/packages/helm/shini4i/argo-watcher) for deploying the server.

Helm chart values configurations example

```yaml
# credentials to access ArgoCD
argo:
  url: https://argocd.argocd.svc.cluster.local
  # although using admin user is not a problem, we recommend creating a separate user for Argo Watcher
  # policy.csv example:
  # p, role:watcher, applications, get, */*, allow
  # p, role:watcher, applications, sync, */*, allow
  # g, watcher, role:watcher
  username: watcher
  # secret with ARGO_PASSWORD key
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

Argo Watcher Server supports the following environment variables

| Variable          | Description                                                     | Mandatory |
|-------------------|-----------------------------------------------------------------|-----------|
| STATE_TYPE        | Accepts "in-memory" (non-HA option) and "postgres" (HA option). | Yes       |
| STATIC_FILES_PATH | Path to the UI website of Argo Watcher                          | Yes       |
| ARGO_URL          | ArgoCD URL                                                      | Yes       |
| ARGO_USER         | ArgoCD API User                                                 | Yes       |
| ARGO_PASSWORD     | ArgoCD API Password                                             | Yes       |
| ARGO_TIMEOUT      | Time that Argo Watcher is allowed to wait for deployment.       | No        |
| ARGO_API_TIMEOUT  | Timeout for ArgoCD API calls. Defaults to 60 seconds            | No        |
| SKIP_TLS_VERIFY   | Skip SSL verification during API calls                          | No        |
| HOST              | Host for Argo Watcher server. Defaults to 0.0.0.0               | No        |
| PORT              | Port for Argo Watcher server. Defaults to 8080                  | No        |
| DB_HOST           | Database host (Required for STATE_TYPE=postgres)                | No        |
| DB_PORT           | Database port (Required for STATE_TYPE=postgres)                | No        |
| DB_NAME           | Database name (Required for STATE_TYPE=postgres)                | No        |
| DB_USER           | Database username(Required for STATE_TYPE=postgres)             | No        |
| DB_PASSWORD       | Database password (Required for STATE_TYPE=postgres)            | No        |


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
  needs: [build]
  # wait only on main and only when build was successfull
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
      when: success
```

Argo Watcher Client supports the following environment variables

| Variable         | Description                                                            | Mandatory |
|------------------|------------------------------------------------------------------------|-----------|
| ARGO_WATCHER_URL | The url of argo-watcher instance                                       | Yes       |
| ARGO_APP         | The name of argo app to check for images rollout                       | Yes       |
| COMMIT_AUTHOR    | The person who made commit/triggered pipeline                          | Yes       |
| PROJECT_NAME     | An identificator of the business project (not related to argo project) | Yes       |
| IMAGES           | A list of images (separated by ",") that should contain specific tag   | Yes       |
| IMAGE_TAG        | An image tag that is expected to be rolled out                         | Yes       |
| DEBUG            | Print various debug information                                        | No        |
