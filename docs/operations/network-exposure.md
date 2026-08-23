# Network Exposure

Argo Watcher ships with no authentication. A default install serves the Web UI, the full deployment history and an open deployment endpoint to anyone who can reach its port, so where you put it matters as much as how you configure it.

Decide the network boundary before you follow the `ingress` block in the [installation guide](../guides/install.md#install-the-server) — TLS there protects the traffic, not the API.

## What is open without a credential

With `OIDC_ENABLED` unset — the default — every endpoint below answers any caller that can reach the port.

| Endpoint | What a caller gets |
|---|---|
| `POST /api/v1/tasks` | Accepts a deployment (see below) |
| `GET /api/v1/tasks` | The whole history: application, author, project, images and tags |
| `GET /api/v1/tasks/{id}` | The same fields for one task |
| `GET /api/v1/config` | Every non-secret server setting: `argo_cd_url` and its alias, `registry_proxy_url` — both of which usually name an internal host — the OIDC issuer, client id and `privileged_groups`, the lockdown schedule, the state backend type, and the timeouts. Tokens, secrets and the database DSN are excluded |
| `GET /api/v1/reachability`, `GET /api/v1/version`, `GET /api/v1/deploy-lock` | Argo CD and database reachability, the build version, the lock state |
| `/ws` | The deploy-lock and reachability transitions, as they happen |
| `/metrics` | Every application name Argo Watcher has seen, with deployment counts, outcomes and durations |
| `/livez`, `/readyz` | Up/down only |
| `/`, `/swagger` | The Web UI and the API specification |

### What an anonymous submission can do

`POST /api/v1/tasks` takes an [optional credential](../reference/api.md#submitting-a-task), so a request without one is still accepted. It does **not** deploy anything: the git write-back is skipped, and the task cannot supersede an in-flight deployment that did present a credential. What it does do, for up to its `timeout`:

- Polls Argo CD against the server's own `ARGO_TOKEN`, once per interval — asking it to reconcile first, unless `ARGO_REFRESH_APP` is off.
- Fires the start and result [notifications](../guides/notifications.md) to your webhook or Mattermost channel.
- Adds a permanent row to the deployment history, whatever application name it claimed.

It cannot mint a metric series, though: no per-application metric is labelled until Argo CD confirms the application exists, so an invented name lands on the labelless `accepted_deployments` and `unconfirmed_deployment_failures` counters instead ([why](observability.md)).

A caller *with* a valid credential deploys for real — the write-back commits the tag it was given, unvalidated against any registry.

The payload is [bounded](../reference/api.md#submitting-a-task) so one request cannot ask for unbounded work, and the [cross-origin gate](../reference/api.md#cross-origin-requests) stops a page on an unrelated site from submitting through a visitor's browser. Neither is a substitute for a credential: `curl` sends no `Origin`.

## What enabling OIDC closes

[OIDC](../guides/oidc.md) closes the reads only the Web UI consumes — `GET /api/v1/tasks`, `/version`, `/reachability`, `GET /api/v1/deploy-lock` and `/ws` — and makes the deploy-lock writes available to `OIDC_PRIVILEGED_GROUPS` alone. [Protected endpoints](../guides/oidc.md#protected-endpoints) is the full table.

Four things stay open by design, and OIDC does not change any of them:

- **`POST /api/v1/tasks`.** The credential remains optional, because a pipeline that commits image tags elsewhere is a supported setup. Enabling OIDC does not close the submission endpoint.
- **`GET /api/v1/config`.** The Web UI reads the issuer and client id from it before it can hold a token.
- **`GET /api/v1/tasks/{id}`.** Open until you also set [`OIDC_REQUIRE_TASK_READ_AUTH=true`](../guides/oidc.md#closing-the-task-lookup), which every pipeline must be ready for.
- **`/metrics`, `/livez`, `/readyz`.** Prometheus and the kubelet cannot perform an OIDC flow.

## What to put in front

Argo Watcher expects something else to decide who reaches it. In rough order of what most installs need:

- **Keep it off the public internet unless you need it there.** Cluster-internal access, a VPN, or an identity-aware proxy in front of the ingress all remove the whole table above from public reach. This is the only control that covers `POST /api/v1/tasks`.
- **Enable [OIDC](../guides/oidc.md)** whenever the Web UI has more than one user, then set `OIDC_REQUIRE_TASK_READ_AUTH=true` once every pipeline sends a credential. The [`unauthenticated_reads`](observability.md#tracking-the-read-auth-migration) metric tells you when that is true.
- **Restrict `/metrics` at the ingress** to your Prometheus. Application names are usually the most sensitive thing a default install publishes, and nothing in Argo Watcher gates that route.
- **Terminate TLS.** The deploy token and CI JWT travel in a header on every submission and every status poll. The client refuses a redirect that steps down from `https` to `http`, but it cannot rescue a plain-`http` endpoint you pointed it at.

!!! warning "A published instance with no proxy in front is a deploy trigger anyone can pull"
    Anyone who can reach the ingress can submit tasks, read every application name you deploy, and — with a leaked deploy token or CI JWT — commit an arbitrary image tag to your GitOps repository. Neither the payload caps nor the cross-origin gate authenticates anybody.
