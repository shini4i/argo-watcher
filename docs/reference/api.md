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

With OIDC **enabled**, the endpoints the Web UI consumes require one: `GET /api/v1/tasks`, `/version`, `/reachability` and `/deploy-lock`. Any of the credentials above is accepted; group membership is not required for reads. The `/ws` WebSocket is gated too; a browser passes its token as the `argo-watcher.token.<token>` subprotocol, since it cannot set a header on the handshake. Two endpoints stay deliberately open — `GET /api/v1/tasks/{id}` (the lookup the client polls, guarded only by the unguessable task id) and `GET /api/v1/config` (the Web UI bootstraps its login from it). See [Protected endpoints](../guides/oidc.md#protected-endpoints) for the full table and the caveats.

A read rejected for a missing or invalid credential returns `401 Unauthorized`. A read whose credential could not be checked because the OIDC provider was unreachable returns `503 Service Unavailable` — the distinction exists so a provider outage does not read as a dead session.

A task submitted **without** a credential is still accepted (`202 Accepted`) and its rollout is monitored normally — argo-watcher simply does not perform the git write-back. It also cannot cancel a deployment that was submitted with one: superseding only applies to in-flight tasks that presented no more authority than the new task, so an uncredentialed submission can never abort a credentialed deployment's write-back. This is the expected setup when the image tag is updated by other means (e.g. Argo CD Image Updater or your CI pipeline) and argo-watcher only tracks the resulting rollout. If you instead rely on the built-in updater to commit the tag and omit the credential, the write-back is skipped and the deployment times out waiting for an image change that never arrives. A token that is **present but invalid or expired** returns `401 Unauthorized`.

The state-changing `POST`/`DELETE /api/v1/deploy-lock` endpoints are only registered when [OIDC authentication](../guides/oidc.md) is enabled, and require a session belonging to one of the `OIDC_PRIVILEGED_GROUPS` (sent via the `Oidc-Authorization` header; the legacy `Keycloak-Authorization` header is still accepted); when OIDC is disabled these endpoints are not exposed (`404 Not Found`). The read-only `GET /api/v1/deploy-lock` needs no privileged group, but with OIDC enabled it does need a credential like the other reads above.

## Conventions

- All request and response bodies are JSON.
- Successful task submissions return `202 Accepted` with the new task ID.
- Validation failures return `406 Not Acceptable` with an `error` field describing the problem.
- Authentication failures return `401 Unauthorized` with an `error` field describing whether no credentials were provided or the token was rejected.
- Unexpected server-side problems return `500 Internal Server Error`.

## Endpoints

The full endpoint catalog is rendered live from the OpenAPI spec maintained alongside the source code. Use the explorer below to inspect routes, request and response schemas, and try requests against your own server.

<swagger-ui src="swagger.json"/>

## Swagger UI bundled with the server

The Argo Watcher server also bundles the same Swagger UI at `/swagger/index.html`, which is convenient when working against a deployed instance:

```text
https://argo-watcher.example.com/swagger/index.html
```
