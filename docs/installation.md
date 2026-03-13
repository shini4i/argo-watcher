# Installation

This guide covers deploying the Argo Watcher server in Kubernetes and integrating the client into your CI/CD pipeline.

## Prerequisites

Before installing Argo Watcher, ensure you have the following:

- A running **Kubernetes cluster** with [Argo CD](https://argo-cd.readthedocs.io/) installed
- **Helm 3** installed on your local machine
- An **Argo CD API token** for the Argo Watcher service account (see [Argo CD API token](#argo-cd-api-token) below)
- *(Optional)* A **PostgreSQL** database for persistent task storage

### Argo CD API Token

Argo Watcher needs an API token to communicate with Argo CD. While using the admin user works, it is recommended to create a dedicated service account with minimal permissions.

Add the following to your Argo CD `argocd-rbac-cm` ConfigMap:

```yaml
policy.csv: |
  p, role:watcher, applications, get, */*, allow
  p, role:watcher, applications, sync, */*, allow
  g, watcher, role:watcher
```

Then create the `watcher` account and generate a token using the Argo CD CLI:

```bash
argocd account generate-token --account watcher
```

## Server Installation

Argo Watcher server is designed to run in a Kubernetes environment. You can deploy it using the official [Helm chart](https://artifacthub.io/packages/helm/shini4i/argo-watcher).

### Helm Chart

Add the Helm repository and install the chart:

```bash
helm repo add shini4i https://shini4i.github.io/helm-charts
helm repo update
helm install argo-watcher shini4i/argo-watcher -f values.yaml
```

Below is an example `values.yaml` configuration:

```yaml
# Credentials for accessing Argo CD
argo:
  url: https://argocd.argocd.svc.cluster.local
  # Secret containing the ARGO_TOKEN key
  # Optionally include ARGO_WATCHER_DEPLOY_TOKEN (must match the client-side value)
  secretName: "argo-watcher"
  # Required only if using the built-in GitOps updater
  updater:
    sshSecretName: "ssh-secret"

# PostgreSQL configuration for persistent task storage
# Omit this section to use in-memory storage (non-HA, data lost on restart)
postgres:
  enabled: true
  host: argo-watcher-postgresql.argo-watcher-postgresql.svc.cluster.local
  name: argo-watcher
  user: argo-watcher
  secretName: "argo-watcher-postgresql"

# Ingress configuration for the API and Web UI
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

### Environment Variables

The Argo Watcher server supports the following environment variables. When using the Helm chart, most of these are set through chart values automatically.

#### Core Settings

| Variable            | Description                                                     | Default     | Required    |
|---------------------|-----------------------------------------------------------------|-------------|-------------|
| `ARGO_URL`          | Argo CD server URL                                              |             | Yes         |
| `ARGO_TOKEN`        | Argo CD API token                                               |             | Yes         |
| `ARGO_API_TIMEOUT`    | Timeout for Argo CD API calls, in seconds                       | `60`        | No          |
| `DEPLOYMENT_TIMEOUT`  | Maximum time (in seconds) to wait for a deployment to complete  | `900`       | No          |
| `ARGO_REFRESH_APP`    | Refresh the application during status checks                    | `true`      | No          |
| `ARGO_API_RETRIES`    | Total retry attempts for Argo CD API calls (1-10)               | `3`         | No          |
| `ACCEPT_SUSPENDED_APP`| Accept "Suspended" health status as valid                       | `false`     | No          |
| `STATE_TYPE`          | Storage backend: `in-memory` (non-HA) or `postgres` (HA)       |             | Yes         |

#### Server Settings

| Variable            | Description                                                     | Default     | Required    |
|---------------------|-----------------------------------------------------------------|-------------|-------------|
| `HOST`              | Host address for the Argo Watcher server                        | `0.0.0.0`  | No          |
| `PORT`              | Port for the Argo Watcher server                                | `8080`      | No          |
| `STATIC_FILES_PATH` | Path to the Web UI static files                                 | `static`    | No          |
| `SKIP_TLS_VERIFY`     | Skip TLS certificate verification for API calls                 | `false`     | No          |
| `DOCKER_IMAGES_PROXY` | Registry proxy URL for image existence checks                   |             | No          |
| `ARGO_URL_ALIAS`      | URL alias for generating externally visible Argo CD app links   |             | No          |

#### Logging

| Variable     | Description                                                            | Default | Required |
|--------------|------------------------------------------------------------------------|---------|----------|
| `LOG_LEVEL`  | Log verbosity level (`debug`, `info`, `warn`, `error`)                | `info`  | No       |

#### Database Settings

These variables are required when `STATE_TYPE` is set to `postgres`.

| Variable      | Description       | Default     | Required      |
|---------------|-------------------|-------------|---------------|
| `DB_HOST`     | Database host     |             | Conditional   |
| `DB_PORT`     | Database port     |             | Conditional   |
| `DB_NAME`     | Database name     |             | Conditional   |
| `DB_USER`     | Database username |             | Conditional   |
| `DB_PASSWORD` | Database password |             | Conditional   |
| `DB_SSL_MODE` | PostgreSQL SSL mode | `disable` | No            |
| `DB_TIMEZONE` | Database timezone | `UTC`       | No            |

#### Git Integration Settings

These variables are required when using the built-in GitOps updater. See the [Git Integration](git-integration.md) guide for full details.

| Variable              | Description                                             | Default | Required    |
|-----------------------|---------------------------------------------------------|---------|-------------|
| `SSH_KEY_PATH`        | Path to the SSH key for Git repository access           |         | Conditional |
| `SSH_KEY_PASS`        | Passphrase for the SSH key                              |         | No          |
| `SSH_COMMIT_USER`     | Git commit author name                                  | `argo-watcher` | No          |
| `SSH_COMMIT_MAIL`     | Git commit author email                                 | `argo-watcher@example.com` | No          |
| `COMMIT_MESSAGE_FORMAT` | Go template string for commit messages                |         | No          |

### Database Setup

When using PostgreSQL for persistent storage (`STATE_TYPE=postgres`), the database must be initialized before starting the server.

#### Using the Helm Chart

If you deploy PostgreSQL alongside Argo Watcher using the Helm chart, migrations are applied automatically via an init container.

#### Manual Migration

If you manage your database separately, run the migrations using the [golang-migrate](https://github.com/golang-migrate/migrate) tool:

```bash
migrate -path db/migrations \
  -database "postgresql://<user>:<password>@<host>:<port>/<dbname>?sslmode=disable" up
```

!!! tip
    The project includes a Docker Compose setup with automatic migrations for local development. See the [Development](development.md) guide for details.

## Client Setup

The Argo Watcher client is a lightweight CLI tool that communicates with the Argo Watcher server. It is distributed as a Docker image at [`ghcr.io/shini4i/argo-watcher-client`](https://ghcr.io/shini4i/argo-watcher-client).

### Client Environment Variables

| Variable                    | Description                                                                                       | Required |
|-----------------------------|---------------------------------------------------------------------------------------------------|----------|
| `ARGO_WATCHER_URL`          | URL of the Argo Watcher server instance                                                          | Yes      |
| `ARGO_APP`                  | Name of the Argo CD application to monitor                                                       | Yes      |
| `COMMIT_AUTHOR`             | Person who triggered the deployment                                                              | Yes      |
| `PROJECT_NAME`              | Identifier for the business project (not the Argo CD project)                                    | Yes      |
| `IMAGES`                    | Comma-separated list of image names expected to contain the specified tag                        | Yes      |
| `IMAGE_TAG`                 | Image tag expected to be deployed                                                                | Yes      |
| `ARGO_WATCHER_DEPLOY_TOKEN` | Deploy token for Git image override (required when using the built-in GitOps updater)            | No       |
| `BEARER_TOKEN`              | JWT token for authentication (prefix with `Bearer `, e.g. `Bearer <token>`)                     | No       |
| `TIMEOUT`                   | HTTP request timeout (e.g. `60s`, `2m`)                                                         | No       |
| `TASK_TIMEOUT`              | Maximum time (in seconds) to wait for a task to complete                                        | No       |
| `RETRY_INTERVAL`            | Interval between status polling attempts (e.g. `15s`, `1m`)                                    | No       |
| `EXPECTED_DEPLOY_TIME`      | Expected deployment duration; affects polling behavior (e.g. `15m`, `30m`)                     | No       |
| `DEBUG`                     | Enable verbose debug output                                                                      | No       |

### GitLab CI/CD

Below is a complete GitLab CI/CD example that builds an image with Kaniko and monitors the deployment with Argo Watcher.

```yaml
stages:
  - deploy

# Build a new Docker image
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
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

# Monitor deployment with Argo Watcher
watch:
  stage: deploy
  image: ghcr.io/shini4i/argo-watcher-client:<VERSION>
  variables:
    ARGO_WATCHER_URL: https://argo-watcher.example.com
    ARGO_APP: example
    COMMIT_AUTHOR: $GITLAB_USER_EMAIL
    PROJECT_NAME: $CI_PROJECT_PATH
    IMAGES: $CI_REGISTRY_IMAGE
    IMAGE_TAG: $CI_COMMIT_TAG
    DEBUG: "1"
  script:
    - /client
  needs: [build]
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
      when: success
```

!!! warning
    Replace `<VERSION>` with a specific Argo Watcher release tag (e.g., `v0.8.0`). Avoid using `latest` in production.

### GitHub Actions

Below is an equivalent example for GitHub Actions.

```yaml
name: Deploy and Monitor

on:
  push:
    branches: [main]

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Build and push Docker image
        # Your preferred Docker build step here
        run: |
          docker build -t ${{ vars.REGISTRY_IMAGE }}:${{ github.sha }} .
          docker push ${{ vars.REGISTRY_IMAGE }}:${{ github.sha }}

      - name: Monitor deployment
        run: |
          docker run --rm \
            -e ARGO_WATCHER_URL=https://argo-watcher.example.com \
            -e ARGO_APP=example \
            -e COMMIT_AUTHOR="${{ github.actor }}" \
            -e PROJECT_NAME="${{ github.repository }}" \
            -e IMAGES="${{ vars.REGISTRY_IMAGE }}" \
            -e IMAGE_TAG="${{ github.sha }}" \
            -e DEBUG=1 \
            ghcr.io/shini4i/argo-watcher-client:<VERSION>
```

## Troubleshooting

### Server does not start

- Verify that `ARGO_URL` and `ARGO_TOKEN` are set correctly and that the server can reach the Argo CD API.
- If using `STATE_TYPE=postgres`, ensure the database is accessible and migrations have been applied.
- Check the server logs by setting `LOG_LEVEL=debug` for verbose output.

### Client exits with a non-zero code

- Confirm that the `ARGO_WATCHER_URL` is reachable from the CI runner.
- Check that the `ARGO_APP` name matches an application registered in Argo CD.
- Verify that the `IMAGES` and `IMAGE_TAG` values correspond to the image that was actually built and pushed.
- If using the built-in GitOps updater, ensure `ARGO_WATCHER_DEPLOY_TOKEN` or `BEARER_TOKEN` is set.

### Deployment times out

- The default `DEPLOYMENT_TIMEOUT` is `900` seconds (15 minutes). Increase the value to accommodate longer rollouts.
- Verify that Argo CD can detect and sync the updated image tag. Check the Argo CD UI for sync errors.
- If using the built-in GitOps updater, ensure the SSH key has write access to the target repository.

### Web UI is not accessible

- Confirm that the Ingress resource is configured correctly and the TLS certificate is valid.
- Verify that `STATIC_FILES_PATH` points to the correct directory containing the built UI assets.
