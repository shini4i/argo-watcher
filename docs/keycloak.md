# Keycloak integration

## Keycloak

As a prerequisites, we need to have a fully configured realm. Keycloak configuration itself is out of scope of this documentation.

The main requirement is the content of the token. We expect the token to contain `groups` field.
Argo-Watcher will compare it with the pre-configured priveleged groups to understand who should see a redeploy button.

## Argo-Watcher

The following environment variables are required to enable Keycloak integration:

- `KEYCLOAK_URL` - the url of Keycloak instance, it will enable Keycloak integration
- `KEYCLOAK_REALM` - the name of the realm
- `KEYCLOAK_CLIENT_ID` - the name of the client
- `KEYCLOAK_TOKEN_VALIDATION_INTERVAL` - the interval in nanoseconds to validate the token (default: 10000)
- `KEYCLOAK_PRIVILEGED_GROUPS` - the comma-separated list of groups that should see the redeploy button

The usage is quite simple, with enabled Keycloak integration, Argo-Watcher will force all users to login with Keycloak before they can see any tasks.

Additionally, if the user is in the group listed in `KEYCLOAK_PRIVILEGED_GROUPS`, Argo-Watcher will show the redeploy button on the task details page.

## Roadmap

Currently, we have a very limited configuration options for Keycloak integration. Eventually, we plan to implement the following features:

-  [x] Keycloak token validation on the backend side, to remove the necessity to pass the deploy token in the UI
-  [ ] Add RBAC support to restrict redeploy support on a per-application basis
