# OIDC / SSO Integration

Argo Watcher supports any [OpenID Connect](https://openid.net/connect/) (OIDC) provider — such as [Keycloak](https://www.keycloak.org/) or [Authentik](https://goauthentik.io/) — for user authentication and group-based access control. When enabled, all users must authenticate through your provider before accessing the Web UI or performing privileged operations.

## How It Works

When OIDC integration is enabled:

1. Users are redirected to the provider's login page before they can view any tasks in the Web UI.
2. The user's token is validated by the Argo Watcher backend by calling the provider's **userinfo** endpoint, which the backend discovers automatically from the issuer's [discovery document](https://openid.net/specs/openid-connect-discovery-1_0.html) (`<issuer>/.well-known/openid-configuration`).
3. Users who belong to one of the configured **privileged groups** see a **Redeploy** button on the task details page and can manage the [deployment lock](gitops-updater.md#deployment-locking).

!!! note
    The backend discovers the userinfo endpoint lazily, on the first token validation — not at startup — so a provider that is briefly unreachable when Argo Watcher boots does not prevent it from starting.

## Prerequisites

You need a fully configured OIDC provider with a **public** client application (Authorization Code + PKCE) set up for Argo Watcher. Provider configuration is outside the scope of this guide — refer to your provider's documentation.

The key requirement is that the token **and** the userinfo response must include a `groups` claim. Argo Watcher uses this claim to determine group membership for privilege checks.

!!! tip "Keycloak"
    Add a `groups` claim by creating a **Group Membership** protocol mapper in the client configuration. Set the **Token Claim Name** to `groups` and enable it for the userinfo endpoint.

!!! tip "Authentik"
    Create a **Scope Mapping** that emits a `groups` claim and attach it to one of the scopes Argo Watcher actually requests — `profile` or `email`. Argo Watcher only ever requests `openid profile email`, so a mapping gated behind a separate `groups` scope is never evaluated and group membership will come back empty.

## Configuration

The following environment variables control the OIDC integration:

| Variable                         | Description                                                              | Default | Required    |
|----------------------------------|--------------------------------------------------------------------------|---------|-------------|
| `OIDC_ENABLED`                   | Enable OIDC authentication                                               | `false` | No          |
| `OIDC_ISSUER_URL`                | The provider's issuer URL (used for discovery)                           |         | Conditional |
| `OIDC_CLIENT_ID`                 | Client ID registered with the provider                                   |         | Conditional |
| `OIDC_TOKEN_VALIDATION_INTERVAL` | Interval (in milliseconds) between token validations                     | `10000` | No          |
| `OIDC_PRIVILEGED_GROUPS`         | Comma-separated list of groups with elevated permissions                 |         | Conditional |

All `Conditional` variables are required when `OIDC_ENABLED` is set to `true`.

!!! note
    `OIDC_TOKEN_VALIDATION_INTERVAL` is in milliseconds. The default of `10000` checks token validity every 10 seconds.

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
