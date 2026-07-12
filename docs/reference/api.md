# API Reference

Argo Watcher exposes a REST API for managing deployment tasks, querying status, and controlling the deployment lock. The API is served by the Argo Watcher server, typically at port `8080`.

## Base URL

All API endpoints are prefixed with `/api/v1` unless otherwise noted.

```text
https://argo-watcher.example.com/api/v1
```

## Authentication

Authentication is only required to authorize the built-in [GitOps Updater](../guides/gitops-updater.md)'s git write-back. Provide one of:

- **Deploy token** — Pass the `ARGO_WATCHER_DEPLOY_TOKEN` value as a query parameter or header.
- **JWT token** — Include a `Bearer` token in the `Authorization` header.

A task submitted **without** a credential is still accepted (`202 Accepted`) and its rollout is monitored normally — argo-watcher simply does not perform the git write-back. This is the expected setup when the image tag is updated by other means (e.g. Argo CD Image Updater or your CI pipeline) and argo-watcher only tracks the resulting rollout. If you instead rely on the built-in updater to commit the tag and omit the credential, the write-back is skipped and the deployment times out waiting for an image change that never arrives. A token that is **present but invalid or expired** returns `401 Unauthorized`.

If [Keycloak](../guides/keycloak.md) is enabled, additional endpoints (such as deploy lock) require a valid Keycloak session.

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
