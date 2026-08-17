# Installation

Deploy the Argo Watcher server on Kubernetes with the official Helm chart, then run the client in your pipeline. The chart is the supported install path — it owns the environment variables, probes, migrations, and volumes described here.

## Prerequisites

- A Kubernetes cluster with [Argo CD](https://argo-cd.readthedocs.io/) installed
- Helm 3
- An Argo CD API token (below)
- PostgreSQL, if you want task history to survive restarts or to run more than one replica

### Argo CD API token

Argo Watcher needs an Argo CD token. Use a dedicated account rather than `admin`. Add to the `argocd-rbac-cm` ConfigMap:

```yaml
policy.csv: |
  p, role:watcher, applications, get, */*, allow
  p, role:watcher, applications, sync, */*, allow
  g, watcher, role:watcher
```

Then create the `watcher` account and mint a token:

```bash
argocd account generate-token --account watcher
```

## Install the server

```bash
helm repo add shini4i https://shini4i.github.io/charts/
helm repo update
helm install argo-watcher shini4i/argo-watcher -f values.yaml
```

A minimal `values.yaml`:

```yaml
argo:
  url: https://argocd.argocd.svc.cluster.local
  # Secret holding ARGO_TOKEN, plus ARGO_WATCHER_DEPLOY_TOKEN if you use the updater
  secretName: "argo-watcher"
  # DEPLOYMENT_TIMEOUT in seconds; the chart's default is 300, not the binary's 900
  timeout: 300

# Built-in GitOps updater (optional)
updater:
  sshSecretName: "ssh-secret"

# Persistent task storage. Omit to run in-memory (single replica, lost on restart)
postgres:
  enabled: true
  host: argo-watcher-postgresql.argo-watcher-postgresql.svc.cluster.local
  name: argo-watcher
  user: argo-watcher
  secretName: "argo-watcher-postgresql"

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

The chart maps its own values onto the server's environment variables and exposes `extraEnvs` for anything it does not cover. Every variable is listed in [Server Environment Variables](../reference/server-env.md); OIDC has [its own values block](oidc.md#helm-chart-values).

!!! note
    The chart sets `livenessProbe` to `/livez` and `readinessProbe` to `/readyz`, which is the correct pairing — see [Health and probe endpoints](../reference/api.md#health-and-probe-endpoints) before overriding either.

### Database setup

With `postgres.enabled: true` the chart runs migrations for you: a `pre-install`/`pre-upgrade` hook Job executes `argo-watcher --migrate` before the server starts. `helm install` and `helm upgrade` are all you need.

If you manage the database outside the chart, apply the migrations yourself with [golang-migrate](https://github.com/golang-migrate/migrate):

```bash
migrate -path db/migrations \
  -database "postgresql://<user>:<password>@<host>:<port>/<dbname>?sslmode=disable" up
```

See [Database](../operations/database.md) for the schema, backups, and sizing.

## Run the client in CI

The client is a container image: [`ghcr.io/shini4i/argo-watcher-client`](https://ghcr.io/shini4i/argo-watcher-client). It reads its configuration from the environment ([full list](../reference/client-env.md)) and exits non-zero when the deployment does not succeed.

Pin a release tag rather than `latest`.

=== "GitLab CI"

    ```yaml
    stages:
      - deploy

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
      script:
        - /client
      needs: [build]
      rules:
        - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    ```

=== "GitHub Actions"

    ```yaml
    name: Deploy and Monitor

    on:
      push:
        branches: [main]

    jobs:
      build-and-deploy:
        runs-on: ubuntu-latest
        steps:
          - uses: actions/checkout@v4

          - name: Build and push image
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
                ghcr.io/shini4i/argo-watcher-client:<VERSION>
    ```

Set `DEBUG=1` while wiring this up: the client then logs the equivalent cURL commands, with credentials redacted.

## Next steps

- [GitOps Updater](gitops-updater.md) — let Argo Watcher commit the image tag.
- [Notifications](notifications.md) — report deployments to Slack, Mattermost, or a webhook.
- [OIDC / SSO](oidc.md) — put the Web UI behind your identity provider.
- [Troubleshooting](../operations/troubleshooting.md) — what to do when a deployment does not behave.
