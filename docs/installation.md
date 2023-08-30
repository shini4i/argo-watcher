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
  # secret with ARGO_TOKEN key and optional ARGO_WATCHER_DEPLOY_TOKEN (should match ARGO_WATCHER_DEPLOY_TOKEN on client side)
  secretName: "argo-watcher"
  # the following values are required only if you want to use Argo Watcher to manage deployments
  updater:
    # A secret containing ssh key that would be used to interact with git repositories
    sshSecretName: "ssh-secret"

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

| Variable            | Description                                                          | Default   | Mandatory   |
|---------------------|----------------------------------------------------------------------|-----------|-------------|
| ARGO_URL            | ArgoCD URL                                                           |           | Yes         |
| ARGO_TOKEN          | ArgoCD API token                                                     |           | Yes         |
| ARGO_API_TIMEOUT    | Timeout for ArgoCD API calls. Defaults to 60 seconds                 | 60        | No          |
| ARGO_TIMEOUT        | Time that Argo Watcher is allowed to wait for deployment.            | 0         | No          |
| ARGO_REFRESH_APP    | Refresh application during status check                              | true      | No          |
| DOCKER_IMAGES_PROXY | Define registry proxy url for image checks                           |           | No          |
| STATE_TYPE          | Accepts "in-memory" (non-HA option) and "postgres" (HA option).      |           | Yes         |
| STATIC_FILES_PATH   | Path to the UI website of Argo Watcher                               | static    | No          |
| SKIP_TLS_VERIFY     | Skip SSL verification during API calls                               | false     | No          |
| LOG_LEVEL           | Severity for logging (trace,debug,info,warn,error,fatal, panic)      | info      | No          |
| LOG_FORMAT          | json (used for production by default) or text (used for development) | json      | No          |
| HOST                | Host for Argo Watcher server.                                        | 0.0.0.0   | No          |
| PORT                | Port for Argo Watcher server.                                        | 8080      | No          |
| DB_HOST             | Database host (Required for STATE_TYPE=postgres)                     | localhost | Conditional |
| DB_PORT             | Database port (Required for STATE_TYPE=postgres)                     | 5432      | Conditional |
| DB_NAME             | Database name (Required for STATE_TYPE=postgres)                     |           | Conditional |
| DB_USER             | Database username(Required for STATE_TYPE=postgres)                  |           | Conditional |
| DB_PASSWORD         | Database password (Required for STATE_TYPE=postgres)                 |           | Conditional |
| SSH_KEY_PATH        | Path to ssh key that would be used to interact with git repositories |           | Conditional |
| SSH_KEY_PASS        | Password for ssh key                                                 |           | No          |
| SSH_COMMIT_USER     | Git user name for commit                                             |           | No          |
| SSH_COMMIT_EMAIL    | Git user email for commit                                            |           | No          |

# Client setup

The client is designed to run on Kubernetes runners. We have
a [dedicated docker image](https://ghcr.io/shini4i/argo-watcher-client) for Argo Watcher Client CI/CD jobs.

## Running on GitLab CI/CD

Example deployment setup for running with GitLab CI/CD (
reference: https://docs.gitlab.com/ee/ci/docker/using_kaniko.html)

```yaml
# we have only deployment stage
stages:
  - deploy

# build new image
build:
  stage: deploy
  image:
    name: gcr.io/kaniko-project/executor:v1.9.0-debug
    entrypoint: [ "" ]
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
  image: ghcr.io/shini4i/argo-watcher:<VERSION>
  variables:
    ARGO_WATCHER_URL: https://argo-watcher.example.com
    ARGO_APP: example
    COMMIT_AUTHOR: $GITLAB_USER_EMAIL
    PROJECT_NAME: $CI_PROJECT_PATH
    IMAGES: $CI_REGISTRY_IMAGE
    IMAGE_TAG: $CI_COMMIT_TAG
    DEBUG: 1
  script:
    - /argo-watcher -client
  # run after we build the image
  needs: [ build ]
  # wait only on main and only when build was successfull
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
      when: success
```

Argo Watcher Client supports the following environment variables

| Variable                  | Description                                                                                                               | Mandatory |
|---------------------------|---------------------------------------------------------------------------------------------------------------------------|-----------|
| ARGO_WATCHER_URL          | The url of argo-watcher instance                                                                                          | Yes       |
| ARGO_APP                  | The name of argo app to check for images rollout                                                                          | Yes       |
| COMMIT_AUTHOR             | The person who made commit/triggered pipeline                                                                             | Yes       |
| PROJECT_NAME              | An identificator of the business project (not related to argo project)                                                    | Yes       |
| IMAGES                    | A list of images (separated by ",") that should contain specific tag                                                      | Yes       |
| IMAGE_TAG                 | An image tag that is expected to be rolled out                                                                            | Yes       |
| ARGO_WATCHER_DEPLOY_TOKEN | A token to enable git image override (required when argo watcher is managing deployments instead of argocd image updater) | No        |
| DEBUG                     | Print various debug information                                                                                           | No        |
