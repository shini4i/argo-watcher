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

# Built-in GitOps updater configuration (optional)
updater:
  sshSecretName: "ssh-secret"

# PostgreSQL configuration for persistent task storage
# Omit or set enabled: false to use in-memory storage (non-HA, data lost on restart)
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

All server environment variables are documented in the [Server Environment Variables](../reference/server-env.md) reference page. When using the Helm chart, most variables are set through chart values automatically.

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
    The project includes a Docker Compose setup with automatic migrations for local development. See the [Development](../contributing/development.md) guide for details.

## Client Setup

The Argo Watcher client is a lightweight CLI tool distributed as a Docker image at [`ghcr.io/shini4i/argo-watcher-client`](https://ghcr.io/shini4i/argo-watcher-client).

All client environment variables are documented in the [Client Environment Variables](../reference/client-env.md) reference page.

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
      --destination "${CI_REGISTRY_IMAGE}:${CI_COMMIT_SHORT_SHA}"
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
    IMAGE_TAG: $CI_COMMIT_SHORT_SHA
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
