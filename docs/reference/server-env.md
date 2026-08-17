# Server Environment Variables

Every setting the server reads from the environment. With the Helm chart most of these are set for you from chart values; anything the chart has no value for goes in `extraEnvs`.

On startup all missing or invalid variables are reported together in one error, so a broken configuration takes one restart to fix, not one per variable.

## Core

| Variable | Description | Default | Required |
|---|---|---|---|
| `ARGO_URL` | Argo CD server URL | | Yes |
| `ARGO_TOKEN` | Argo CD API token | | Yes |
| `STATE_TYPE` | Storage backend: `in-memory` (single replica) or `postgres` | | Yes |
| `DEPLOYMENT_TIMEOUT` | Seconds to wait for a deployment to finish | `900` | No |
| `ARGO_API_TIMEOUT` | Timeout for Argo CD API calls, in seconds | `60` | No |
| `ARGO_API_RETRIES` | Total attempts per Argo CD API call (1–10) | `3` | No |
| `ARGO_REFRESH_APP` | Refresh the application during status checks | `true` | No |
| `ACCEPT_SUSPENDED_APP` | Treat a `Suspended` health status as deployed | `false` | No |

Turning `ARGO_REFRESH_APP` off also disables the [fail-fast image check](../operations/troubleshooting.md#image-is-not-part-of-application), which needs a freshly reconciled application.

## Server

| Variable | Description | Default | Required |
|---|---|---|---|
| `HOST` | Listen address | `0.0.0.0` | No |
| `PORT` | Listen port | `8080` | No |
| `STATIC_FILES_PATH` | Directory holding the built Web UI | `static` | No |
| `LOG_LEVEL` | `debug`, `info`, `warn` or `error` | `info` | No |
| `SKIP_TLS_VERIFY` | Skip TLS verification on outgoing API calls | `false` | No |
| `ARGO_URL_ALIAS` | Externally reachable Argo CD URL, used in generated app links | | No |
| `DOCKER_IMAGES_PROXY` | Registry proxy prefix to tolerate when matching images | | No |
| `REPO_CACHE_PATH` | Where GitOps repository clones are cached | `/data` | No |

The chart mounts its persistent volume at `REPO_CACHE_PATH`; change one and you must change the other.

## Authentication

| Variable | Description | Default | Required |
|---|---|---|---|
| `ARGO_WATCHER_DEPLOY_TOKEN` | Shared token clients present to authorize a write-back | | No |
| `JWT_SECRET` | HMAC secret for validating client JWTs | | No |
| `OIDC_ENABLED` | Enable OIDC authentication | `false` | No |
| `OIDC_ISSUER_URL` | Provider issuer URL, used for discovery | | When OIDC is on |
| `OIDC_CLIENT_ID` | Client id registered with the provider | | When OIDC is on |
| `OIDC_PRIVILEGED_GROUPS` | Groups allowed to roll back a task and manage the deploy lock | | No |
| `OIDC_TOKEN_VALIDATION_INTERVAL` | How long (ms) a provider decision may be reused | `300000` | No |
| `OIDC_REQUIRE_TASK_READ_AUTH` | Require a credential on `GET /api/v1/tasks/{id}` too | `false` | No |

Enabling OIDC also makes the Web UI's read endpoints require a credential — see [Protected endpoints](../guides/oidc.md#protected-endpoints). The legacy `KEYCLOAK_*` variables are still honored but deprecated ([migration table](../guides/oidc.md#migrating-from-keycloak_)).

`OIDC_REQUIRE_TASK_READ_AUTH` fails every deployment driven by a client that sends no credential on reads, which includes **every client older than v0.15.0**. It also requires `OIDC_ENABLED=true`; the server refuses to start otherwise. Check `unauthenticated_reads` first — see [Closing the task lookup](../guides/oidc.md#closing-the-task-lookup).

## Features

| Variable | Description | Default | Required |
|---|---|---|---|
| `LOCKDOWN_SCHEDULE` | Recurring deploy freeze, e.g. `Fri 20:00 - Mon 08:00` | | No |
| `WEBHOOK_ENABLED` | Enable webhook notifications | `false` | No |
| `MATTERMOST_ENABLED` | Enable Mattermost notifications | `false` | No |

The remaining `WEBHOOK_*` and `MATTERMOST_*` variables are documented in [Notifications](../guides/notifications.md); the schedule format in [Deployment Lock](../guides/deployment-lock.md).

## Database

Required when `STATE_TYPE=postgres`. The server builds its DSN from these; `DB_DSN` overrides the result if you need connection parameters the individual variables do not cover.

| Variable | Description | Default |
|---|---|---|
| `DB_HOST` | Database host | |
| `DB_PORT` | Database port | |
| `DB_NAME` | Database name | |
| `DB_USER` | Database user | |
| `DB_PASSWORD` | Database password | |
| `DB_SSL_MODE` | PostgreSQL SSL mode | `disable` |
| `DB_TIMEZONE` | Session timezone | `UTC` |
| `DB_CONNECT_TIMEOUT` | Seconds to wait for the initial connection (at least 1) | `10` |
| `DB_DSN` | Full DSN, replacing the one built from the variables above | built from `DB_*` |
| `DB_MIGRATIONS_PATH` | Migrations directory used by `argo-watcher --migrate` | `/app/db/migrations` |

`DB_CONNECT_TIMEOUT` is enforced even when `DB_DSN` is set explicitly, so an unreachable database fails fast instead of hanging on the OS TCP timeout.

## GitOps updater

Read when the built-in [GitOps updater](../guides/gitops-updater.md) is in use.

| Variable | Description | Default |
|---|---|---|
| `SSH_KEY_PATH` | Private SSH key used to push (required to enable the updater) | |
| `SSH_KEY_PASS` | Passphrase for that key | |
| `SSH_KNOWN_HOSTS` | `known_hosts` file(s) used to verify the remote host, colon-separated | `~/.ssh/known_hosts`, `/etc/ssh/ssh_known_hosts` |
| `SSH_COMMIT_USER` | Commit author name | `argo-watcher` |
| `SSH_COMMIT_MAIL` | Commit author email | `argo-watcher@example.com` |
| `COMMIT_MESSAGE_FORMAT` | Go template for the commit message | built-in format |
| `GIT_OP_TIMEOUT` | Wall-clock budget for **one** clone + update attempt | `90s` |
| `GIT_MAX_ATTEMPTS` | Total attempts (initial + retries) before giving up | `5` |
| `GIT_BATCH_WRITEBACK` | Coalesce concurrent write-backs to one repo | `false` |
| `GIT_BATCH_MAX_SIZE` | Applications committed per batch flush | `20` |
| `GIT_TIMEOUT` | **Deprecated.** Used as `GIT_OP_TIMEOUT` when that is unset | |

Host key verification is on: a remote whose key is not listed fails the push. `SSH_KNOWN_HOSTS` is read by go-git rather than by argo-watcher's own configuration, which is why it has no `GitConfig` field. The chart writes the file and sets the variable for you.

### Retry and timeout budget

`GIT_OP_TIMEOUT` bounds a single attempt, not the whole loop — worst case is `GIT_OP_TIMEOUT × GIT_MAX_ATTEMPTS` plus jittered backoff between attempts (capped-exponential, from roughly 250ms to at most 2s, deliberately tight so a retry can win a push race).

Retries exist for contention on a shared repository: each attempt re-fetches, re-applies and re-pushes, and the final attempt discards the on-disk cache and clones fresh, so a poisoned cache heals itself. Raising `GIT_MAX_ATTEMPTS` cannot let an older deployment overwrite a newer one — a superseded task aborts its write-back before every attempt.

### During shutdown

The whole shutdown sequence has a fixed 25-second budget (it must fit the default 30-second `terminationGracePeriodSeconds`), shared with the HTTP and WebSocket drains. An attempt already running is not interrupted, so at the 90-second default a write-back stuck on an unreachable remote is cut off mid-attempt. Keep `GIT_OP_TIMEOUT` below 25s if in-flight commits should land rather than be abandoned on restart.

This decides whether the **commit** reaches Git, not whether the task's final status is recorded — see [Tasks stay "in progress" after a server restart](../operations/troubleshooting.md#tasks-stay-in-progress-after-a-server-restart).
