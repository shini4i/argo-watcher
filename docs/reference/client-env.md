# Client Environment Variables

The client is a small CLI shipped as [`ghcr.io/shini4i/argo-watcher-client`](https://ghcr.io/shini4i/argo-watcher-client). It reads everything from the environment, submits one task, waits for it, and exits non-zero if the deployment did not succeed.

## Required

| Variable | Description |
|---|---|
| `ARGO_WATCHER_URL` | URL of the Argo Watcher server |
| `ARGO_APP` | Argo CD application to monitor |
| `IMAGES` | Comma-separated image names expected to carry `IMAGE_TAG` |
| `IMAGE_TAG` | Image tag expected to be deployed |
| `COMMIT_AUTHOR` | Who triggered the deployment |
| `PROJECT_NAME` | Business project identifier (not the Argo CD project) |

## Optional

| Variable | Description | Default |
|---|---|---|
| `BEARER_TOKEN` | JWT authorizing the deployment. Takes precedence over `ARGO_WATCHER_DEPLOY_TOKEN`. | |
| `ARGO_WATCHER_DEPLOY_TOKEN` | Shared deploy token, the alternative to a JWT | |
| `TIMEOUT` | Per-request HTTP timeout | `60s` |
| `RETRY_INTERVAL` | Wait between status polls | `15s` |
| `TASK_TIMEOUT` | Seconds the server should wait for this deployment; unset keeps the server's `DEPLOYMENT_TIMEOUT`, and anything above 86400 is clamped to it | |
| `TASK_REFRESH` | `true`/`false` override of the server's `ARGO_REFRESH_APP` for this deployment | |
| `EXPECTED_DEPLOY_TIME` | After this long, the client's log line changes to "taking longer than expected". Nothing else changes. | `15m` |
| `DEBUG` | Log the equivalent cURL commands, with credentials redacted | `false` |

Set `BEARER_TOKEN` to the raw token (`eyJhbGci...`) so CI can mask it; a legacy `Bearer <token>` value is still accepted.

## Authentication

The configured credential is sent on every request — the submission and each status poll. A server running with [`OIDC_REQUIRE_TASK_READ_AUTH`](server-env.md#authentication) therefore accepts this client, and a server without it ignores the extra header.

A credential is dropped rather than forwarded when a redirect changes the host, and the client logs one warning naming both hosts. See [Image tag is never committed](../operations/troubleshooting.md#image-tag-is-never-committed-write-back-skipped).

## Retries

While polling, the client retries transient failures — network errors and `5xx` responses — three times, two seconds apart. Neither is configurable. Terminal failures fail immediately: `4xx` responses, a rejected token, a malformed response, or a redirect that steps down from `https`. The initial `POST` is not retried at all.
