# OIDC / SSO Integration

Argo Watcher supports any [OpenID Connect](https://openid.net/connect/) (OIDC) provider — such as [Keycloak](https://www.keycloak.org/) or [Authentik](https://goauthentik.io/) — for user authentication and group-based access control. When enabled, all users must authenticate through your provider before accessing the Web UI or performing privileged operations.

## How It Works

When OIDC integration is enabled:

1. Users are redirected to the provider's login page before they can view any tasks in the Web UI.
2. The user's token is validated by the Argo Watcher backend by calling the provider's **userinfo** endpoint, which the backend discovers automatically from the issuer's [discovery document](https://openid.net/specs/openid-connect-discovery-1_0.html) (`<issuer>/.well-known/openid-configuration`).
3. The backend **requires a credential on its read endpoints** — being signed in is enough, no group membership needed. See [Protected endpoints](#protected-endpoints).
4. Users who belong to one of the configured **privileged groups** see a **Redeploy** button on the task details page and can manage the [deployment lock](gitops-updater.md#deployment-locking).

With OIDC disabled, every endpoint stays open exactly as before — there is no auth backend to validate against.

!!! note
    The backend discovers the userinfo endpoint lazily, on the first token validation — not at startup — so a provider that is briefly unreachable when Argo Watcher boots does not prevent it from starting.

## Protected endpoints

Enabling OIDC closes the endpoints only the Web UI consumes. Nothing else reads
them, so no deployment pipeline is affected.

| Endpoint | With OIDC enabled |
|----------|-------------------|
| `GET /api/v1/tasks` | Credential required |
| `GET /api/v1/version` | Credential required |
| `GET /api/v1/reachability` | Credential required |
| `GET /api/v1/deploy-lock` | Credential required |
| `POST`/`DELETE /api/v1/deploy-lock` | Credential required **and** privileged group |
| `POST /api/v1/tasks` | Unchanged — optional credential (governs git write-back) |
| `GET /api/v1/tasks/{id}` | **Open** unless `OIDC_REQUIRE_TASK_READ_AUTH=true` — see below |
| `GET /api/v1/config` | **Open** — the Web UI reads the issuer and client id from it before it can obtain a token |
| `/livez`, `/readyz`, `/metrics` | **Open** — probes and Prometheus cannot perform an OIDC flow |
| `/ws` | Credential required — as a subprotocol from a browser, see [The WebSocket handshake](#the-websocket-handshake) |

Any configured credential is accepted on the reads: an OIDC session, the
`ARGO_WATCHER_DEPLOY_TOKEN`, or a [JWT](gitops-updater.md#jwt-configuration).
Read access is deliberately **not** limited to `OIDC_PRIVILEGED_GROUPS` — that gate
applies to the deploy lock only.

!!! warning "`GET /api/v1/tasks/{id}` is open by default"
    The Argo Watcher client polls `GET /api/v1/tasks/{id}` for the whole length of
    every deployment. Requiring a credential there rejects any client too old to send
    one, so the lookup stays open until you close it deliberately with
    `OIDC_REQUIRE_TASK_READ_AUTH` (below). The task id is a random v4 UUID returned
    only to the submitter, and the enumerable `GET /api/v1/tasks` list is protected,
    so the exposure is limited to callers that already hold a task id — but a task id
    appearing in a CI log or a webhook payload is enough to read that task's app,
    author, project, images and status. The `unauthenticated_reads` metric reports how
    many such reads still arrive without a credential; see
    [Tracking the read-auth migration](../operations/observability.md#tracking-the-read-auth-migration).

### Closing the task lookup

The client presents its credential — `ARGO_WATCHER_DEPLOY_TOKEN` or `BEARER_TOKEN`,
whichever is configured — on the status poll as well as on the submission. Once every
pipeline runs a client that does so, set:

```shell
OIDC_REQUIRE_TASK_READ_AUTH=true
```

`GET /api/v1/tasks/{id}` then requires a credential like every other read, leaving
`GET /api/v1/config` as the only open `/api/v1` read (alongside the probe endpoints
and `/metrics`, which cannot perform an OIDC flow).

Two things to know before flipping it:

- **It rejects pipelines that send no credential.** A client that submits tasks without
  a token still gets its task accepted, then fails on the first status poll with `401`.
  Wait until `unauthenticated_reads` has stayed at zero across a full deployment cycle
  for every project — that counter exists to answer exactly this question.
- **A zero counter means a credential was sent, not that it was accepted.** The counter
  records presence only, so a wrong deploy token reads as migrated. A `BEARER_TOKEN`
  JWT must also outlive the longest deployment: it is checked on every status poll
  once this is on, not only at submission.
- **It requires `OIDC_ENABLED=true`.** With OIDC disabled no read is protected, so the
  server refuses to start rather than accept a setting that would not take effect.

A provider outage is still reported as `503`, which the client retries, rather than
`401`, which it treats as terminal.

### The WebSocket handshake

`/ws` requires a credential like the other reads — it broadcasts the deployment-lock and
Argo CD reachability transitions that `GET /api/v1/deploy-lock` and
`GET /api/v1/reachability` are gated on, so leaving it open would make gating those
cosmetic. No task data crosses the socket.

A browser cannot attach a header to a WebSocket handshake — the API takes a URL and a
subprotocol list — so the Web UI offers its token as a subprotocol instead:

```text
Sec-WebSocket-Protocol: argo-watcher.v1, argo-watcher.token.<access-token>
```

The server negotiates `argo-watcher.v1` and never echoes the token entry. Clients that
can set headers (the CLI, monitoring probes) send `Oidc-Authorization`,
`ARGO_WATCHER_DEPLOY_TOKEN` or `Authorization` as usual and need no subprotocol. A
handshake carrying no credential is refused with `401` before the connection is
upgraded; one whose provider cannot be reached is refused with `503`.

### If the provider is unreachable

A read whose credential could not be checked because the provider itself is
unreachable returns **503 Service Unavailable**, not 401. The Web UI treats a 401 as
a dead session and signs the user out, so a brief provider outage must not be
reported as an authentication failure. Cached decisions (see
`OIDC_TOKEN_VALIDATION_INTERVAL` below) keep working during the outage.

## Prerequisites

You need a fully configured OIDC provider with a **public** client application (Authorization Code + PKCE) set up for Argo Watcher. Two client settings are dictated by how the Web UI signs in, and are covered below; anything else is provider-specific — refer to your provider's documentation.

### Redirect URI and web origin

The Web UI signs in with a **top-level redirect** (not a hidden iframe), so the provider must allow the browser back:

- **Redirect URI** — the application's base URL **including the trailing slash**, e.g. `https://argo-watcher.example.com/`. When Argo Watcher is served under a sub-path, include it: `https://example.com/argo-watcher/`. The same value is used as the post-logout redirect URI. Providers match this exactly, and a mismatch fails the login on the provider's own error page, before Argo Watcher is ever reached.
- **Web origin** — the application's origin (scheme, host, port; no path). After the redirect the browser reads group membership straight from the provider's userinfo endpoint, and that request carries an `Authorization` header, so the provider must allow it cross-origin. Providers differ here: Keycloak has a separate **Web origins** field, while others derive it from the registered redirect URI.

    Get this wrong and **sign-in still succeeds** — the failure surfaces later, and quietly: the userinfo call is blocked, group membership comes back empty, and every privileged control stays hidden. That is the same symptom as a missing `groups` claim below, so check both when the rollback button or the deploy-lock toggle does not appear for a user who should have them.

`test/keycloak/argo-watcher-e2e-realm.json` in the repository is a minimal working client definition — public, authorization code with PKCE (`S256`), an exact redirect URI, a web origin, and the `groups` mapper below. The browser test suite signs in against it on every run, so it stays a correct reference.

!!! tip "Keycloak"
    Set **Valid redirect URIs** and **Web origins** in the client's settings. Both are needed: with Web origins empty, users sign in normally and then have no privileged controls.

!!! tip "Authentik"
    Set **Redirect URI** on the OAuth2/OIDC provider to the same value.

!!! note
    The provider commonly runs on a different domain than Argo Watcher. That is supported — the flow is a top-level redirect precisely so it does not depend on third-party cookies — but it does mean the userinfo call is cross-origin, which is what the web origin setting is for.

### The `groups` claim

Every provider must satisfy one requirement: emit a `groups` claim in **both** the ID token and the **userinfo** response. Both are used: the Web UI gates its buttons on userinfo, the same source the backend independently enforces on, and falls back to the ID-token claim when that call fails. The backend has no such fallback — while userinfo is unreachable it refuses every privileged action, so the UI may still show a rollback button or a deploy-lock toggle that the API then rejects. The claim must be gated behind a scope Argo Watcher actually requests. Argo Watcher only ever requests `openid profile email`, so the claim must be bound to `profile` or `email` (or the base `openid` scope) — a claim gated behind a separate `groups` scope is never evaluated, and group membership comes back empty.

The steps below are the same requirement expressed in each provider's configuration model.

!!! tip "Keycloak"
    Create a **Group Membership** protocol mapper in the client configuration. Set the **Token Claim Name** to `groups` and enable **Add to userinfo**. Attach the mapper to the client directly or to a default client scope (`profile`/`email`) so it is always included.

!!! tip "Authentik"
    Create a **Scope Mapping** that emits a `groups` claim and attach it to the `profile` or `email` scope. Do **not** put it behind a dedicated `groups` scope: Authentik only evaluates a scope mapping when its scope is requested, and Argo Watcher never requests a `groups` scope, so the claim would never be emitted.

## Configuration

The following environment variables control the OIDC integration:

| Variable                         | Description                                                              | Default | Required    |
|----------------------------------|--------------------------------------------------------------------------|---------|-------------|
| `OIDC_ENABLED`                   | Enable OIDC authentication                                               | `false` | No          |
| `OIDC_ISSUER_URL`                | The provider's issuer URL (used for discovery)                           |         | Conditional |
| `OIDC_CLIENT_ID`                 | Client ID registered with the provider                                   |         | Conditional |
| `OIDC_TOKEN_VALIDATION_INTERVAL` | How long (in milliseconds) a provider decision about a token may be reused | `300000` | No          |
| `OIDC_PRIVILEGED_GROUPS`         | Comma-separated list of groups with elevated permissions                 |         | Conditional |

All `Conditional` variables are required when `OIDC_ENABLED` is set to `true`.

### `OIDC_TOKEN_VALIDATION_INTERVAL`

Every authorization decision is made by asking the provider's userinfo endpoint
about the token. Since the Web UI refreshes on a timer, doing that per request
would turn each open browser tab into a steady stream of provider traffic, so a
decision is reused for this interval — one userinfo call per token per interval,
regardless of how many requests arrive.

The interval is the upper bound on how stale a **read** authorization can be. Each
decision is additionally capped by the token's own expiry, so it can never outlive
the credential it describes. Setting it to `0` validates every request against the
provider.

Privileged actions — managing the deployment lock — are exempt: they always
re-verify against the provider, so removing a user from `OIDC_PRIVILEGED_GROUPS`
takes effect immediately rather than within the interval. A lock click is a rare
human action, so the extra round trip costs nothing.

!!! note
    The default is `300000` (5 minutes), matched to typical access-token lifetimes.

### The issuer URL

`OIDC_ISSUER_URL` is the value the provider advertises as its `issuer`. Argo Watcher appends `/.well-known/openid-configuration` to it to discover the userinfo endpoint.

| Provider  | Typical issuer URL                                        |
|-----------|-----------------------------------------------------------|
| Keycloak  | `https://keycloak.example.com/realms/<realm>`             |
| Authentik | `https://authentik.example.com/application/o/<app-slug>/` |

### Example: Keycloak

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://keycloak.example.com/realms/your-realm
OIDC_CLIENT_ID=argo-watcher
OIDC_PRIVILEGED_GROUPS=platform-team,sre-team
```

### Example: Authentik

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://authentik.example.com/application/o/argo-watcher/
OIDC_CLIENT_ID=argo-watcher
OIDC_PRIVILEGED_GROUPS=platform-team,sre-team
```

### Helm Chart Values

When deploying with the Helm chart, set the configuration via `extraEnvs` in `values.yaml`:

```yaml
extraEnvs:
  - name: OIDC_ENABLED
    value: "true"
  - name: OIDC_ISSUER_URL
    value: "https://keycloak.example.com/realms/your-realm"
  - name: OIDC_CLIENT_ID
    value: "argo-watcher"
  - name: OIDC_PRIVILEGED_GROUPS
    value: "platform-team,sre-team"
```

## Migrating from `KEYCLOAK_*`

Earlier releases were Keycloak-specific and used `KEYCLOAK_*` variables. These are **deprecated but still honored** — existing deployments keep working with no change. When the `OIDC_*` variables are unset, Argo Watcher maps the legacy ones automatically and logs a one-time deprecation warning:

| Deprecated                           | Replacement                                                                 |
|--------------------------------------|-----------------------------------------------------------------------------|
| `KEYCLOAK_ENABLED`                   | `OIDC_ENABLED`                                                              |
| `KEYCLOAK_URL` + `KEYCLOAK_REALM`    | `OIDC_ISSUER_URL` (synthesized as `<KEYCLOAK_URL>/realms/<KEYCLOAK_REALM>`) |
| `KEYCLOAK_CLIENT_ID`                 | `OIDC_CLIENT_ID`                                                            |
| `KEYCLOAK_TOKEN_VALIDATION_INTERVAL` | `OIDC_TOKEN_VALIDATION_INTERVAL`                                            |
| `KEYCLOAK_PRIVILEGED_GROUPS`         | `OIDC_PRIVILEGED_GROUPS`                                                    |

`OIDC_*` takes precedence when both are set. The `/api/v1/config` endpoint continues to expose the auth block under a legacy `keycloak` key (mirroring the new `oidc` key) for backward compatibility.

## Privileged Groups

Users in privileged groups receive additional capabilities in the Web UI:

- **Redeploy button** — Visible on the task details page, allowing privileged users to trigger a redeployment.
- **Deployment lock management** — Privileged users can enable or disable the [deployment lock](gitops-updater.md#deployment-locking) via the Web UI.

## Future Improvements

- [ ] RBAC support to restrict redeployment on a per-application basis
