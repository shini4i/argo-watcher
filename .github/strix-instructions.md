# Strix scope and context for argo-watcher

argo-watcher watches Argo CD applications and reports whether a deployment
reached the requested image. CI clients submit a deploy task over the HTTP API;
a React UI and a Prometheus scrape target sit on the same server.

The running target is `http://172.17.0.1:8080`. It serves both the JSON API
under `/api/v1` and the built UI as static files. The OpenAPI description of
every endpoint is at `/swagger/index.html`, and the generated spec at
`web/public/swagger/swagger.json`.

**Only port 8080 is a target.** `172.17.0.1` is the CI runner itself, reached
over the Docker bridge. Port `8090` is the throwaway Keycloak described below:
use it to mint tokens, do not attack it. Anything else listening on that address
is the build machine, not this application — do not probe it, do not report it.

**An unrouted path is not a 404.** Anything the API does not serve — an unknown
path *or* an unknown method on a known path — falls through to the single-page
app handler and comes back `200 OK` with `index.html`. An HTML body on an
`/api/v1` path means the route is not registered, never that you reached it
unauthenticated. Check the body before reporting access to any endpoint.

## Authentication

OIDC is on, backed by a throwaway Keycloak at `http://172.17.0.1:8090`, realm
`argo-watcher-e2e`, public client `argo-watcher`. The issuer is pinned to that
address — a token minted against any other name for the same server will be
rejected. Two users exist:

| user           | password       | groups       |
|----------------|----------------|--------------|
| `priv-user`    | `priv-pass`    | `privileged` |
| `regular-user` | `regular-pass` | none         |

Mint a token with the password grant. `scope=openid` is mandatory — Keycloak 26
answers userinfo `403` without it:

```
POST http://172.17.0.1:8090/realms/argo-watcher-e2e/protocol/openid-connect/token
grant_type=password&client_id=argo-watcher&scope=openid&username=priv-user&password=priv-pass
```

Send it to argo-watcher in the `Oidc-Authorization: Bearer <token>` header — not
`Authorization`, which carries deploy tokens and JWTs instead. A second client
`other-app` exists in the realm; tokens it issues are meant to be rejected.

## Where the value is

Authorization is the interesting surface. Worth proving or disproving:

- `regular-user` writing or releasing the deploy lock, which is gated on
  `privileged` group membership.
- A token minted for the `other-app` client being accepted by argo-watcher.
- One caller cancelling, hijacking, or reading another's task.
- Injection or traversal through task fields that reach Argo CD, git write-back,
  or notification delivery.
- Anything in the changed files of this pull request.

## Accepted by design — do not report these

- `POST /api/v1/tasks` takes no credential. CI clients submit anonymously; the
  route is bounded by request-size and field validation instead.
- `GET /api/v1/config` is always open: the UI reads it to bootstrap its login.
- `GET /api/v1/tasks/{id}` is open unless `OIDC_REQUIRE_TASK_READ_AUTH` is set,
  which it is not here.
- `GET /metrics` is an unauthenticated Prometheus scrape target, secured at the
  network layer rather than in the application.
- `/api/v1/app-tokens` is not registered at all in this configuration — it needs
  `STATE_TYPE=postgres` and this instance runs in-memory. It answers with the UI
  per the fallback rule above; that is not an unauthenticated token store.
- `ARGO_URL` points at a closed port, so every Argo CD call fails. That is the
  lab, not a defect.
- Keycloak on 8090 is a disposable dev-mode container with a bootstrap admin of
  `admin`/`admin`. Its configuration, credentials and version are out of scope.
- The Keycloak credentials above are public test fixtures.
