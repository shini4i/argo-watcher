# API Reference

Argo Watcher exposes a REST API for managing deployment tasks, querying status, and controlling the deployment lock. The API is served by the Argo Watcher server, typically at port `8080`.

## Base URL

All API endpoints are prefixed with `/api/v1` unless otherwise noted.

```text
https://argo-watcher.example.com/api/v1
```

## Authentication

Credentials serve two purposes: authorizing the built-in [GitOps Updater](../guides/gitops-updater.md)'s git write-back, and — when [OIDC authentication](../guides/oidc.md) is enabled — reading the API at all. Provide one of:

- **Deploy token** — Pass the `ARGO_WATCHER_DEPLOY_TOKEN` value in the header of the same name.
- **JWT token** — Pass the JWT in the `Authorization` header. The raw token is accepted directly (e.g. `Authorization: eyJhbGci...`); a `Bearer <token>` value is also accepted for backward compatibility.
- **OIDC session** — Pass the access token in the `Oidc-Authorization` header (the legacy `Keycloak-Authorization` name is still accepted).

### Read access

With OIDC **disabled** — the default — every endpoint is readable without a credential.

With OIDC **enabled**, the endpoints the Web UI consumes require one: `GET /api/v1/tasks`, `/version`, `/reachability` and `/deploy-lock`. Any of the credentials above is accepted; group membership is not required for reads. The `/ws` WebSocket is gated too; a browser passes its token as the `argo-watcher.token.<token>` subprotocol, since it cannot set a header on the handshake. Two endpoints stay open by default — `GET /api/v1/tasks/{id}` (the lookup the client polls, guarded only by the unguessable task id) and `GET /api/v1/config` (the Web UI bootstraps its login from it). Setting `OIDC_REQUIRE_TASK_READ_AUTH=true` gates the task lookup as well, which requires every client polling it to present a credential; see [Closing the task lookup](../guides/oidc.md#closing-the-task-lookup). `GET /api/v1/config` cannot be gated. See [Protected endpoints](../guides/oidc.md#protected-endpoints) for the full table and the caveats.

A read rejected for a missing or invalid credential returns `401 Unauthorized`. A read whose credential could not be checked because the OIDC provider was unreachable returns `503 Service Unavailable` — the distinction exists so a provider outage does not read as a dead session.

A task submitted **without** a credential is still accepted (`202 Accepted`) and its rollout is monitored normally — argo-watcher simply does not perform the git write-back. It also cannot cancel a deployment that was submitted with one: superseding only applies to in-flight tasks that presented no more authority than the new task, so an uncredentialed submission can never abort a credentialed deployment's write-back. This is the expected setup when the image tag is updated by other means (e.g. Argo CD Image Updater or your CI pipeline) and argo-watcher only tracks the resulting rollout. If you instead rely on the built-in updater to commit the tag and omit the credential, the write-back is skipped and the deployment times out waiting for an image change that never arrives. A token that is **present but invalid or expired** returns `401 Unauthorized`.

The state-changing `POST`/`DELETE /api/v1/deploy-lock` endpoints are only registered when [OIDC authentication](../guides/oidc.md) is enabled, and require a session belonging to one of the `OIDC_PRIVILEGED_GROUPS` (sent via the `Oidc-Authorization` header; the legacy `Keycloak-Authorization` header is still accepted); when OIDC is disabled these endpoints are not registered at all, and the request falls through to the Web UI's catch-all handler — so it answers `200 OK` with an HTML body rather than an API error. Check the `Content-Type`, not the status code, to tell "not exposed" from a successful call. The read-only `GET /api/v1/deploy-lock` needs no privileged group, but with OIDC enabled it does need a credential like the other reads above.

## Conventions

- All request and response bodies are JSON.
- Successful task submissions return `202 Accepted` with the new task ID.
- Validation failures return `406 Not Acceptable` with an `error` field describing the problem.
- Authentication failures return `401 Unauthorized` with an `error` field describing whether no credentials were provided or the token was rejected.
- Unexpected server-side problems return `500 Internal Server Error`.

## Health and probe endpoints

Two unauthenticated endpoints report server health. They answer different questions,
and wiring the wrong one to a Kubernetes probe has real consequences.

| Endpoint | Checks | Use as |
|----------|--------|--------|
| `GET /livez` | Nothing but that the process is still serving requests | Liveness probe |
| `GET /readyz` | Not shutting down, **and** the state backend answers | Readiness probe |

Both return `{"status":"up"}` on success and `503` with `{"status":"down","reason":"..."}`
otherwise — `shutting down` during a graceful shutdown, `state backend unreachable` when
the database cannot be pinged.

!!! warning "Do not point a liveness probe at `/readyz`"
    A liveness failure gets the container restarted, and a restart cannot reach a
    database that is down. Probing the state backend for liveness turns a recoverable
    outage into a fleet-wide `CrashLoopBackoff` while every replica is still able to
    serve task history and the unreachable banner. That is why `/livez` checks no
    dependency at all.

A startup probe is not needed and is not recommended. The server binds its listener
only after its configuration, Argo CD client, and state backend are all initialised, so
there is no window in which it answers requests but is not yet ready. If the state
backend is unreachable at boot the process exits rather than starting degraded, which a
startup probe could not observe anyway.

**Argo CD reachability is deliberately absent from both probes.** An Argo CD outage
degrades Argo Watcher — new deployments are rejected fast and the Web UI shows a banner
naming the cause — but the server must keep serving precisely then. Alert on the
`argocd_unavailable` metric or poll `GET /api/v1/reachability` instead.

### Readiness during shutdown

On `SIGTERM` the server fails `/readyz` immediately, keeps serving normally for five
seconds so the endpoint removal can propagate through kube-proxy and any ingress
controller, and only then closes its listener and drains in-flight requests, WebSocket
connections, and queued git write-backs. A readiness probe is what makes that window
useful; without one, traffic keeps arriving at a pod that has already stopped
accepting it. The whole sequence is bounded to fit the default 30-second
`terminationGracePeriodSeconds`.

## Endpoints

The full endpoint catalog is rendered live from the OpenAPI spec maintained alongside the source code. Use the explorer below to inspect routes, request and response schemas, and try requests against your own server.

<swagger-ui src="swagger.json"/>

## Swagger UI bundled with the server

The Argo Watcher server also bundles the same Swagger UI at `/swagger/index.html`, which is convenient when working against a deployed instance:

```text
https://argo-watcher.example.com/swagger/index.html
```
