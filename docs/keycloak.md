# Keycloak Integration

Argo Watcher supports [Keycloak](https://www.keycloak.org/) for user authentication and role-based access control. When enabled, all users must authenticate through Keycloak before accessing the Web UI or performing privileged operations.

## How It Works

When Keycloak integration is enabled:

1. Users are redirected to the Keycloak login page before they can view any tasks in the Web UI.
2. The user's token is validated by the Argo Watcher backend at a configurable interval.
3. Users who belong to one of the configured **privileged groups** see a **Redeploy** button on the task details page and can manage the [deployment lock](git-integration.md#deployment-locking).

## Prerequisites

You need a fully configured Keycloak realm with a client application set up for Argo Watcher. Keycloak realm configuration is outside the scope of this guide -- refer to the [Keycloak documentation](https://www.keycloak.org/documentation) for setup instructions.

The key requirement is that the Keycloak token must include a `groups` claim. Argo Watcher uses this claim to determine group membership for privilege checks.

!!! tip
    In Keycloak, you can add a `groups` claim to your client's token by creating a **Group Membership** protocol mapper in the client configuration. Set the **Token Claim Name** to `groups`.

## Configuration

The following environment variables control the Keycloak integration:

| Variable                             | Description                                              | Default | Required    |
|--------------------------------------|----------------------------------------------------------|---------|-------------|
| `KEYCLOAK_ENABLED`                   | Enable Keycloak authentication                           | `false` | No          |
| `KEYCLOAK_URL`                       | URL of the Keycloak instance                             |         | Conditional |
| `KEYCLOAK_REALM`                     | Name of the Keycloak realm                               |         | Conditional |
| `KEYCLOAK_CLIENT_ID`                 | Client ID registered in Keycloak                         |         | Conditional |
| `KEYCLOAK_TOKEN_VALIDATION_INTERVAL` | Interval (in milliseconds) between token validations     | `10000` | No          |
| `KEYCLOAK_PRIVILEGED_GROUPS`         | Comma-separated list of groups with elevated permissions  |         | Conditional |

!!! note
    The `KEYCLOAK_TOKEN_VALIDATION_INTERVAL` value is in milliseconds. The default of `10000` means token validity is checked every 10 seconds. Verify that this value is appropriate for your environment.

All `Conditional` variables are required when `KEYCLOAK_ENABLED` is set to `true`.

### Helm Chart Values

When deploying with the Helm chart, set the Keycloak configuration via `extraEnvs` in `values.yaml`:

```yaml
extraEnvs:
  - name: KEYCLOAK_ENABLED
    value: "true"
  - name: KEYCLOAK_URL
    value: "https://keycloak.example.com"
  - name: KEYCLOAK_REALM
    value: "your-realm"
  - name: KEYCLOAK_CLIENT_ID
    value: "argo-watcher"
  - name: KEYCLOAK_PRIVILEGED_GROUPS
    value: "platform-team,sre-team"
```

## Privileged Groups

Users in privileged groups receive additional capabilities in the Web UI:

- **Redeploy button** -- Visible on the task details page, allowing privileged users to trigger a redeployment.
- **Deployment lock management** -- Privileged users can enable or disable the [deployment lock](git-integration.md#deployment-locking) via the Web UI.

## Future Improvements

- [ ] RBAC support to restrict redeployment on a per-application basis
