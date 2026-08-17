# API Reference

The server exposes a REST API on its own port (default `8080`), with every endpoint under `/api/v1` except the probes, `/metrics`, `/ws` and `/swagger`.

## Conventions

- Request and response bodies are JSON.
- A submitted task returns `202 Accepted` with its id.
- Validation failures return `406 Not Acceptable` with an `error` field naming the problem.
- Authentication failures return `401 Unauthorized`, and `503 Service Unavailable` when the credential could not be checked because the OIDC provider was unreachable.
- Server-side problems return `500 Internal Server Error`.

## Authentication

A credential does two things: it authorizes the [GitOps updater](../guides/gitops-updater.md)'s write-back, and — when [OIDC](../guides/oidc.md) is enabled — it is what lets you read the API at all. Any one of:

| Credential | How to send it |
|---|---|
| Deploy token | `ARGO_WATCHER_DEPLOY_TOKEN: <token>` |
| JWT | `Authorization: <token>` (raw; `Bearer <token>` also accepted) |
| OIDC session | `Oidc-Authorization: <access token>` (legacy `Keycloak-Authorization` also accepted) |

### Reads

With OIDC **disabled** — the default — every endpoint is readable without a credential.

With OIDC **enabled**, the endpoints the Web UI consumes require one, group membership is not needed, and two stay open on purpose: `GET /api/v1/tasks/{id}` (guarded by the unguessable task id, and closable with `OIDC_REQUIRE_TASK_READ_AUTH`) and `GET /api/v1/config`, which the Web UI reads before it can hold a token. [Protected endpoints](../guides/oidc.md#protected-endpoints) has the full table.

### Submitting a task

`POST /api/v1/tasks` takes an **optional** credential:

- **With a valid credential** the task is authorized: the git write-back runs, and the task can supersede an in-flight deployment of the same images.
- **Without one** the task is still accepted (`202`) and monitored normally — the expected setup when image tags are committed elsewhere (Argo CD Image Updater, your pipeline). The write-back is skipped, and it cannot cancel a deployment that did present a credential.
- **With an invalid or expired one** the request is rejected `401`.

An unauthorized task on an application that relies on the built-in updater fails in a way that does not name the credential: usually `Image "<name>" is not part of application "<app>"`, or a timeout when image validation is off. [Image tag is never committed](../operations/troubleshooting.md#image-tag-is-never-committed-write-back-skipped) explains how to confirm it.

### Managing the deploy lock

`POST` and `DELETE /api/v1/deploy-lock` are **registered only when OIDC is enabled**, and require a session in one of the `OIDC_PRIVILEGED_GROUPS`. With OIDC disabled they are not routes at all: the request falls through to the Web UI's catch-all and answers `200 OK` with an HTML body. Check the `Content-Type`, not the status code, to tell "not exposed" from a successful call.

`GET /api/v1/deploy-lock` needs no privileged group, but with OIDC enabled it needs a credential like every other read.

## Health and probe endpoints

Two unauthenticated endpoints report health. They answer different questions, and wiring the wrong one to a probe has consequences.

| Endpoint | Checks | Use as |
|---|---|---|
| `GET /livez` | Only that the process is still serving | Liveness probe |
| `GET /readyz` | Not shutting down, **and** the state backend answers | Readiness probe |

Both return `{"status":"up"}`, or `503` with `{"status":"down","reason":"..."}` — `shutting down` during a graceful shutdown, `state backend unreachable` when the database cannot be pinged.

!!! warning "Never point a liveness probe at `/readyz`"
    A liveness failure restarts the container, and a restart cannot fix a database that is down. Probing the state backend for liveness turns a recoverable outage into a fleet-wide `CrashLoopBackoff` while every replica could still serve task history and the unreachable banner. That is why `/livez` checks no dependency.

A startup probe is unnecessary: the server binds its listener only after its configuration, Argo CD client and state backend are initialised, and exits rather than starting degraded.

**Argo CD reachability is deliberately absent from both probes.** An Argo CD outage degrades Argo Watcher — deployments are rejected fast and the Web UI shows a banner — but the server must keep serving precisely then. Alert on the `argocd_unavailable` metric or poll `GET /api/v1/reachability` instead.

### Readiness during shutdown

On `SIGTERM` the server fails `/readyz` immediately, then keeps serving normally for five seconds so the endpoint removal can propagate through kube-proxy and any ingress controller, and only then closes its listener and drains in-flight requests, WebSocket connections, and queued git write-backs. Without a readiness probe that window is wasted and traffic keeps arriving at a pod that has stopped accepting it. The whole sequence is bounded to fit the default 30-second `terminationGracePeriodSeconds`.

## Endpoints

Rendered from the OpenAPI spec generated from the handlers, so it cannot drift from the code. Expand a route to see its schemas, or try it against your own server.

<swagger-ui src="swagger.json"/>

The server bundles the same explorer at `/swagger/index.html`, which is handier against a live instance.
