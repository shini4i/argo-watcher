# Server Environment Variables

The Argo Watcher server supports the following environment variables. When using the Helm chart, most of these are set through chart values automatically.

## Core Settings

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

## Server Settings

| Variable            | Description                                                     | Default     | Required    |
|---------------------|-----------------------------------------------------------------|-------------|-------------|
| `HOST`              | Host address for the Argo Watcher server                        | `0.0.0.0`  | No          |
| `PORT`              | Port for the Argo Watcher server                                | `8080`      | No          |
| `STATIC_FILES_PATH` | Path to the Web UI static files                                 | `static`    | No          |
| `SKIP_TLS_VERIFY`     | Skip TLS certificate verification for API calls                 | `false`     | No          |
| `DOCKER_IMAGES_PROXY` | Registry proxy URL for image existence checks                   |             | No          |
| `ARGO_URL_ALIAS`      | URL alias for generating externally visible Argo CD app links   |             | No          |

## Logging

| Variable     | Description                                                            | Default | Required |
|--------------|------------------------------------------------------------------------|---------|----------|
| `LOG_LEVEL`  | Log verbosity level (`debug`, `info`, `warn`, `error`)                | `info`  | No       |

## Authentication & Feature Flags

These variables control authentication and optional features. See the linked guides for full configuration details.

| Variable                    | Description                                                                  | Default | Required    |
|-----------------------------|------------------------------------------------------------------------------|---------|-------------|
| `ARGO_WATCHER_DEPLOY_TOKEN` | Shared token for validating client requests. See [GitOps Updater](../guides/gitops-updater.md). |         | No          |
| `JWT_SECRET`                | Secret key for signing and validating JWT tokens. See [GitOps Updater](../guides/gitops-updater.md#jwt-configuration). |         | No          |
| `KEYCLOAK_ENABLED`          | Enable Keycloak authentication. See [Keycloak Integration](../guides/keycloak.md).     | `false` | No          |
| `WEBHOOK_ENABLED`           | Enable webhook notifications. See [Notifications](../guides/notifications.md).         | `false` | No          |
| `LOCKDOWN_SCHEDULE`         | Recurring deployment lock schedule. See [Deployment Locking](../guides/gitops-updater.md#deployment-locking). |         | No          |

## Database Settings

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

## Git Integration Settings

These variables are required when using the built-in GitOps updater. See the [GitOps Updater](../guides/gitops-updater.md) guide for full details.

| Variable              | Description                                             | Default | Required    |
|-----------------------|---------------------------------------------------------|---------|-------------|
| `SSH_KEY_PATH`        | Path to the SSH key for Git repository access           |         | Conditional |
| `SSH_KEY_PASS`        | Passphrase for the SSH key                              |         | No          |
| `SSH_COMMIT_USER`     | Git commit author name                                  | `argo-watcher` | No          |
| `SSH_COMMIT_MAIL`     | Git commit author email                                 | `argo-watcher@example.com` | No          |
| `COMMIT_MESSAGE_FORMAT` | Go template string for commit messages                |         | No          |
