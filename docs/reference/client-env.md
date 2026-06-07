# Client Environment Variables

The Argo Watcher client is a lightweight CLI tool distributed as a Docker image at [`ghcr.io/shini4i/argo-watcher-client`](https://ghcr.io/shini4i/argo-watcher-client). These environment variables control the client's behavior when monitoring deployments.

## Required Variables

| Variable       | Description                                                                                       |
|----------------|---------------------------------------------------------------------------------------------------|
| `ARGO_WATCHER_URL`          | URL of the Argo Watcher server instance                                                          |
| `ARGO_APP`                  | Name of the Argo CD application to monitor                                                       |
| `COMMIT_AUTHOR`             | Person who triggered the deployment                                                              |
| `PROJECT_NAME`              | Identifier for the business project (not the Argo CD project)                                    |
| `IMAGES`                    | Comma-separated list of image names expected to contain the specified tag                        |
| `IMAGE_TAG`                 | Image tag expected to be deployed                                                                |

## Optional Variables

| Variable                    | Description                                                                                       |
|-----------------------------|---------------------------------------------------------------------------------------------------|
| `ARGO_WATCHER_DEPLOY_TOKEN` | Deploy token for Git image override (required when using the built-in GitOps updater)            |
| `BEARER_TOKEN`              | JWT token for authentication (prefix with `Bearer `, e.g. `Bearer <token>`)                     |
| `TIMEOUT`                   | HTTP request timeout (e.g. `60s`, `2m`)                                                         |
| `TASK_TIMEOUT`              | Maximum time (in seconds) to wait for a task to complete                                        |
| `RETRY_INTERVAL`            | Interval between status polling attempts (e.g. `15s`, `1m`)                                    |
| `EXPECTED_DEPLOY_TIME`      | Expected deployment duration; affects polling behavior (e.g. `15m`, `30m`)                     |
| `DEBUG`                     | Enable verbose debug output                                                                      |
