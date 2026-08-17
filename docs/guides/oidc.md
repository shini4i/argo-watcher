# OIDC / SSO

Argo Watcher authenticates users against any [OpenID Connect](https://openid.net/connect/) provider — Keycloak, Authentik, or another — and uses group membership for privileged actions. With OIDC enabled, users sign in before they see any task, and the endpoints the Web UI reads require a credential.

With OIDC disabled (the default) nothing is protected: there is no backend to validate a credential against.

## How it works

1. The browser is redirected to the provider before the Web UI loads any data (Authorization Code + PKCE, a top-level redirect — not a hidden iframe).
2. The backend validates the token by calling the provider's **userinfo** endpoint, which it discovers from `<issuer>/.well-known/openid-configuration`. Discovery happens lazily on the first validation, so a provider that is briefly down at boot does not stop Argo Watcher from starting.
3. Reads the Web UI performs require a credential — being signed in is enough, no group needed.
4. Users in a **privileged group** additionally get the **Rollback to this version** button on a task, and control of the [deployment lock](deployment-lock.md).

The browser performs discovery and the code exchange itself, so the issuer must be reachable **from the browser as well as from the server**. When a sign-in cannot complete, the Web UI stops on its loading screen and names the reason instead of bouncing back to the provider — see [Web UI stops on "Sign-in failed"](../operations/troubleshooting.md#web-ui-stops-on-sign-in-failed).

## Configuration

| Variable | Description | Default | Required |
|---|---|---|---|
| `OIDC_ENABLED` | Turn OIDC on | `false` | No |
| `OIDC_ISSUER_URL` | The provider's issuer URL, used for discovery | | When enabled |
| `OIDC_CLIENT_ID` | Client id registered with the provider | | When enabled |
| `OIDC_PRIVILEGED_GROUPS` | Comma-separated groups allowed to roll back and to manage the lock | | No |
| `OIDC_TOKEN_VALIDATION_INTERVAL` | How long (ms) a provider decision may be reused | `300000` | No |
| `OIDC_REQUIRE_TASK_READ_AUTH` | Also require a credential on `GET /api/v1/tasks/{id}` | `false` | No |

The server refuses to start with `OIDC_ENABLED=true` and no issuer or client id. Leaving `OIDC_PRIVILEGED_GROUPS` empty is valid — it simply means nobody is privileged.

`OIDC_ISSUER_URL` is whatever the provider advertises as its `issuer`:

| Provider | Typical issuer URL |
|---|---|
| Keycloak | `https://keycloak.example.com/realms/<realm>` |
| Authentik | `https://authentik.example.com/application/o/<app-slug>/` |

### Helm chart values

The chart has a dedicated block; `extraEnvs` is not needed:

```yaml
oidc:
  enabled: true
  issuerUrl: "https://keycloak.example.com/realms/your-realm"
  clientId: "argo-watcher"
  privilegedGroups:
    - platform-team
    - sre-team
  # tokenValidationInterval: 300000
```

### `OIDC_TOKEN_VALIDATION_INTERVAL`

Every authorization decision means asking the provider's userinfo endpoint about the token. The Web UI refreshes on a timer, so doing that per request would turn every open tab into a stream of provider traffic; a decision is reused for this interval instead — one userinfo call per token per interval, however many requests arrive.

The interval is the upper bound on how stale a **read** authorization can be, and each decision is additionally capped by the token's own expiry, so it never outlives the credential. `0` validates every request.

Privileged actions are exempt and always re-verify against the provider, so removing a user from `OIDC_PRIVILEGED_GROUPS` takes effect immediately. Clicking a lock toggle is rare enough that the extra round trip costs nothing.

## Provider setup

You need a **public** client (Authorization Code + PKCE). Two settings are dictated by how the Web UI signs in; the rest is provider-specific.

### Redirect URI and web origin

- **Redirect URI** — the application's base URL **including the trailing slash**: `https://argo-watcher.example.com/`, or `https://example.com/argo-watcher/` under a sub-path. The same value is used as the post-logout redirect. Providers match it exactly, and a mismatch fails the login on the provider's own error page.
- **Web origin** — the application's origin (scheme, host, port; no path). After the redirect the browser reads group membership from the userinfo endpoint with an `Authorization` header, so that request must be allowed cross-origin. Keycloak has a separate **Web origins** field; other providers derive it from the redirect URI.

    Get the origin wrong and **sign-in still succeeds** — the failure is quiet: the userinfo call is blocked, group membership comes back empty, and every privileged control stays hidden. That is the same symptom as a missing `groups` claim, so check both when a user who should have the rollback button or the lock switch does not.

!!! tip "Keycloak / Authentik"
    Keycloak: set **Valid redirect URIs** and **Web origins** in the client settings — both are needed. Authentik: set **Redirect URI** on the OAuth2/OIDC provider to the same value.

The provider commonly lives on another domain. That is supported — the flow is a top-level redirect precisely so it does not depend on third-party cookies — but it is also why the web origin matters.

`test/keycloak/argo-watcher-e2e-realm.json` in the repository is a minimal working client definition (public, PKCE `S256`, exact redirect URI, web origin, `groups` mapper). The browser test suite signs in against it on every run, so it stays correct.

### The `groups` claim

The provider must emit a `groups` claim in **both** the ID token and the **userinfo** response. The Web UI gates its buttons on userinfo — the same source the backend independently enforces on — and falls back to the ID-token claim if that call fails. The backend has no fallback: while userinfo is unreachable it refuses every privileged action, so the UI may briefly offer a button the API then rejects.

The claim must be bound to a scope Argo Watcher actually requests. It only ever requests `openid profile email`, so bind it to `profile`, `email`, or `openid` — a claim behind a separate `groups` scope is never emitted.

!!! tip "Keycloak / Authentik"
    Keycloak: create a **Group Membership** mapper with **Token Claim Name** `groups` and **Add to userinfo** enabled, attached to the client or to a default client scope. Authentik: create a **Scope Mapping** emitting `groups` and attach it to `profile` or `email` — not to a dedicated `groups` scope, which is never requested.

## Protected endpoints

Enabling OIDC closes the endpoints only the Web UI consumes, so no pipeline is affected.

| Endpoint | With OIDC enabled |
|---|---|
| `GET /api/v1/tasks` | Credential required |
| `GET /api/v1/version` | Credential required |
| `GET /api/v1/reachability` | Credential required |
| `GET /api/v1/deploy-lock` | Credential required |
| `POST`/`DELETE /api/v1/deploy-lock` | Credential required **and** privileged group |
| `/ws` | Credential required — as a subprotocol from a browser ([why](#the-websocket-handshake)) |
| `POST /api/v1/tasks` | Unchanged — optional credential, which governs the git write-back |
| `GET /api/v1/tasks/{id}` | **Open** unless `OIDC_REQUIRE_TASK_READ_AUTH=true` ([below](#closing-the-task-lookup)) |
| `GET /api/v1/config` | **Open** — the Web UI reads the issuer and client id from it before it can hold a token |
| `/livez`, `/readyz`, `/metrics` | **Open** — probes and Prometheus cannot perform an OIDC flow |

Any configured credential is accepted on a read: an OIDC session, the `ARGO_WATCHER_DEPLOY_TOKEN`, or a [JWT](gitops-updater.md#jwt-configuration). Read access is deliberately **not** limited to `OIDC_PRIVILEGED_GROUPS` — that gate covers the deploy lock only.

!!! warning "`GET /api/v1/tasks/{id}` is open by default"
    The client polls this endpoint for the whole length of every deployment, so requiring a credential rejects any client too old to send one. The task id is a random v4 UUID handed only to the submitter, and the enumerable `GET /api/v1/tasks` list is protected — but anyone holding a task id (from a CI log or a webhook payload) can read that task's app, author, project, images and status. The `unauthenticated_reads` metric counts such reads; see [Tracking the read-auth migration](../operations/observability.md#tracking-the-read-auth-migration).

### Closing the task lookup

The client presents its credential — `ARGO_WATCHER_DEPLOY_TOKEN` or `BEARER_TOKEN`, whichever is set — on status polls as well as on submission, from v0.15.0 onwards. Once every pipeline runs such a client, set:

```shell
OIDC_REQUIRE_TASK_READ_AUTH=true
```

`GET /api/v1/tasks/{id}` then requires a credential like every other read, leaving `GET /api/v1/config` as the only open `/api/v1` read.

Three things to know first:

- **It fails every pipeline that sends no credential.** Such a task is still accepted, then fails on its first status poll with `401`. Wait until `unauthenticated_reads` has stayed at zero across a full deployment cycle for every project.
- **A zero counter means a credential was *sent*, not accepted.** The counter records presence only, so a wrong deploy token reads as migrated. A `BEARER_TOKEN` JWT must also outlive the longest deployment: with this on it is checked on every poll, not only at submission.
- **It requires `OIDC_ENABLED=true`.** Otherwise the server refuses to start rather than accept a setting that could not take effect.

A provider outage is reported as `503`, which the client retries, not `401`, which it treats as terminal.

### The WebSocket handshake

`/ws` is gated like the other reads: it broadcasts the deployment-lock and Argo CD reachability transitions that `GET /api/v1/deploy-lock` and `GET /api/v1/reachability` are gated on, so leaving it open would make gating those cosmetic. No task data crosses the socket.

A browser cannot set a header on a WebSocket handshake — the API takes a URL and a subprotocol list — so the Web UI offers its token as a subprotocol:

```text
Sec-WebSocket-Protocol: argo-watcher.v1, argo-watcher.token.<access-token>
```

The server negotiates `argo-watcher.v1` and never echoes the token entry. Clients that can set headers (the CLI, monitoring probes) send `Oidc-Authorization`, `ARGO_WATCHER_DEPLOY_TOKEN` or `Authorization` as usual. A handshake with no credential is refused `401` before the upgrade; one whose provider cannot be reached is refused `503`.

### When the provider is unreachable

A read whose credential could not be checked returns **503 Service Unavailable**, never 401. The Web UI treats a 401 as a dead session and signs the user out, so a brief provider outage must not look like an authentication failure. Cached decisions keep working throughout.

## Migrating from `KEYCLOAK_*`

Earlier releases were Keycloak-specific. The old variables are **deprecated but still honored** — existing deployments keep working — and are mapped automatically when their `OIDC_*` counterparts are unset, with a one-time deprecation warning in the log.

| Deprecated | Replacement |
|---|---|
| `KEYCLOAK_ENABLED` | `OIDC_ENABLED` |
| `KEYCLOAK_URL` + `KEYCLOAK_REALM` | `OIDC_ISSUER_URL` (synthesized as `<KEYCLOAK_URL>/realms/<KEYCLOAK_REALM>`) |
| `KEYCLOAK_CLIENT_ID` | `OIDC_CLIENT_ID` |
| `KEYCLOAK_TOKEN_VALIDATION_INTERVAL` | `OIDC_TOKEN_VALIDATION_INTERVAL` |
| `KEYCLOAK_PRIVILEGED_GROUPS` | `OIDC_PRIVILEGED_GROUPS` |

`OIDC_*` wins when both are set. `GET /api/v1/config` still mirrors the auth block under a legacy `keycloak` key alongside the new `oidc` one, and both the `Oidc-Authorization` and `Keycloak-Authorization` headers are accepted.

## Privileged groups

Members of `OIDC_PRIVILEGED_GROUPS` get two things: the **Rollback to this version** button on a task page, which is hidden from everyone else, and the [deployment lock](deployment-lock.md) switch, which others see disabled. Restricting rollback per application is not implemented yet.
