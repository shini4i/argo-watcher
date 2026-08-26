# Troubleshooting

Symptom-driven. Each entry names the likely causes, how to confirm which one it is, and the fix. When a problem shows up in monitoring first, [Observability](observability.md) says which metric to look at.

## Server does not start

**Symptom:** the pod never becomes ready, or restarts repeatedly.

**Likely causes**

- `ARGO_URL` or `ARGO_TOKEN` is wrong, or Argo CD is unreachable from the pod.
- A configuration error. The server reports every invalid or missing variable in a single startup error.
- With `STATE_TYPE=postgres`: the database is unreachable, or the migrations have not been applied. Startup fails within `DB_CONNECT_TIMEOUT` seconds (default 10) rather than hanging — look right after the `Connecting to PostgreSQL database...` log line.

**How to verify**

```bash
kubectl logs <pod>                     # the startup error names what is wrong
curl -H "Authorization: Bearer $ARGO_TOKEN" $ARGO_URL/api/v1/applications
```

**Fix**

1. Point `ARGO_URL` at the Argo CD API and mint a fresh token if the current one is rejected.
2. Apply the migrations. With the Helm chart, `helm upgrade` runs them as a hook Job; otherwise run [golang-migrate](https://github.com/golang-migrate/migrate) yourself:

    ```bash
    migrate -path db/migrations -database "$DATABASE_URL" up
    ```

## Client exits with a non-zero code

**Symptom:** the client fails the CI job.

Read the client's last log line first — it distinguishes these:

| Client says | Meaning |
|---|---|
| `The deployment has failed` | Argo CD reported a failure, or the timeout elapsed. Continue with [Deployment times out](#deployment-times-out). |
| `Application <name> does not exist` | `ARGO_APP` does not match an Argo CD application (names are case-sensitive). |
| `Image "<name>" is not part of application` | The application does not declare that image — see [Image is not part of application](#image-is-not-part-of-application). |
| `The deployment was aborted before its outcome could be confirmed` | Argo CD became unreachable during the check. The application itself may be perfectly healthy — check Argo CD before blaming the deployment. |
| `The deployment was cancelled because a newer deployment superseded it` | Expected, not a fault: a newer deployment of the same image took over. Confirm that one succeeded; nothing else to do. |
| `refused to follow a redirect away from https` | See [Client refuses a redirect away from https](#client-refuses-a-redirect-away-from-https). |
| A `503 {"status":"down"}` on submission | Argo CD or the state backend is unreachable, so the server rejects the submission fast instead of letting the client wait. The Web UI shows a banner naming which. |

**Also check**

- `ARGO_WATCHER_URL` is reachable from the runner: `curl $ARGO_WATCHER_URL/readyz`.
- `IMAGES` and `IMAGE_TAG` match what was actually built and pushed.

!!! note
    Transient failures while polling (network errors, `5xx`) are retried three times, two seconds apart, so a single blip is not fatal. A non-zero exit means the failure persisted or was terminal.

## Deployment times out

**Symptom:** the deployment fails on the timeout although the application looks fine in Argo CD.

**Likely causes**

- The timeout is genuinely too short. The binary defaults to 900s, but the **Helm chart sets 300s** via `argo.timeout`.
- Argo CD never received the new tag. When you rely on the built-in updater, the usual reason is a task submitted without a valid credential — see [Image tag is never committed](#image-tag-is-never-committed-write-back-skipped). (If tags are committed by other means, this is not it.)
- The requested image is not part of the application, and the fail-fast check is off. See [Image is not part of application](#image-is-not-part-of-application).

**How to verify**

```bash
kubectl get app <ARGO_APP> -n argocd -o yaml | grep -A5 argo-watcher   # annotations
kubectl logs -l app.kubernetes.io/name=argocd-application-controller   # sync errors
```

**Fix**

1. Raise `argo.timeout` (chart) or `DEPLOYMENT_TIMEOUT` to cover your slowest rollout.
2. If the updater should have committed the tag, confirm the pipeline sets `ARGO_WATCHER_DEPLOY_TOKEN` or `BEARER_TOKEN`, and that the SSH key can write to the repository.

## Image tag is never committed (write-back skipped)

**Symptom:** the server logs `Skipping git repo update: application is managed by the watcher but the task presented no valid credential`, and `gitops_writeback_skipped_unvalidated` rises for that application.

Write-back needs a credential on `POST /api/v1/tasks`. Without one the task is still accepted and monitored — submission is deliberately open — but the tag is never committed, and what the pipeline reports next does not name the real cause:

| Application | What the deployment reports |
|---|---|
| Normal | `Image "<name>" is not part of application "<app>"` — the image really is absent, because the commit never happened. |
| Image validation off (`ARGO_REFRESH_APP=false`, `TASK_REFRESH=false`, or `argo-watcher/skip-image-validation`) | Waits out `DEPLOYMENT_TIMEOUT`, then fails. |
| `argo-watcher/fire-and-forget: "true"` | **Reports success.** The rollout is never checked, so nothing contradicts it. |
| Redeploy of a tag it already runs | Reports success, correctly — there was nothing to commit. |

The server warning and the counter are what identify the credential as the cause.

**Likely causes**

- The pipeline sets neither `ARGO_WATCHER_DEPLOY_TOKEN` nor `BEARER_TOKEN`.
- The value does not match the server's `ARGO_WATCHER_DEPLOY_TOKEN` or `JWT_SECRET`.
- The credential is scoped to other applications. An [application deploy token](../guides/gitops-updater.md#application-deploy-tokens), and a JWT carrying `allowed_apps`, is rejected `401` for an application it does not name — so this shows up as a failed submission, not a skipped write-back.
- The application deploy token was revoked or has expired. Both are reported `401` naming which.
- Application deploy tokens are not available on this server: they need `OIDC_ENABLED=true` and `STATE_TYPE=postgres`, and a token presented without both is rejected `401` naming the missing setting.
- The server sets `JWT_ISSUER` or `JWT_AUDIENCE` and the token carries no such claim, or a different value. Both are strict once set — see [Binding a token to this server](../guides/gitops-updater.md#binding-a-token-to-this-server).
- **Something redirects the client to a different host**, and a credential does not survive that hop. The two behave differently:
    - `Authorization` (`BEARER_TOKEN`) is stripped by Go's HTTP client on every client version, but only when the *hostname* changes. A port-only change (`host:8080` → `host:9090`) keeps it, and so does a move to a subdomain (`example.com` → `sub.example.com`). A sibling host does not: `watcher.example.com` → `watcher.int.example.com` drops it.
    - `ARGO_WATCHER_DEPLOY_TOKEN` is a custom header, which Go always forwards, so from v0.15.0 the client deletes it itself on any change of host **or** port. Earlier clients forwarded it, so this can appear on a client upgrade with nothing else changing.

    Clients from v0.15.0 log one warning naming both hosts when a credential does not survive the hop.

**How to verify**

```bash
curl -sSI "$ARGO_WATCHER_URL/api/v1/config"
```

`200` means there is no redirect. A `301`, `302`, `307` or `308` whose `Location` points at another hostname is the cause. (A `301`/`302` on an API path breaks submission outright, since Go rewrites the `POST` to a `GET`; a `307`/`308` keeps the request and loses only the credential.)

**Fix**

1. Point `ARGO_WATCHER_URL` at the hostname that actually serves the API.
2. If the pipelines cannot change, serve both hostnames from the same ingress instead of redirecting between them. Redirecting browser navigation while passing `/api/v1/*` and `/ws` through keeps the canonical URL in the address bar.

!!! warning
    Do not work around this by letting the credential follow the redirect. The deploy token does not expire, is not scoped to an application, and authorizes commits to your GitOps repository — whoever answers for the redirect target would receive it on every request.

## Client refuses a redirect away from https

**Symptom:** the deployment fails immediately with `refused to follow a redirect away from https`, naming the endpoint that answered and the plain-`http` target it pointed at.

**Meaning:** `ARGO_WATCHER_URL` is an `https` URL but something on the path redirects to plain `http`. Go carries `Authorization` across such a redirect when the hostname is unchanged, so following it would put the deploy token or CI JWT on the wire in the clear. The client refuses and does not retry — this is configuration, not a blip.

**Fix:** point `ARGO_WATCHER_URL` at the endpoint that serves the API over TLS, or stop the ingress from downgrading API requests. Only stepping *down* from TLS is refused; a client deliberately pointed at an `http://` URL keeps working.

## Image is not part of application

**Symptom:** the deployment fails immediately with `Image "<name>" is not part of application "<app>"`, followed by the images the application does declare.

**Meaning:** the application finished rolling out — synced and healthy — but its desired state never declares the requested image, so waiting would only burn the timeout. The desired state comes from Argo CD's managed resources, not from running pods, so a workload with no pod yet (an untriggered `CronJob`, a `Deployment` scaled to zero) still counts as declaring its image.

**Likely causes**

- The image name is misspelled, or its registry prefix does not match the manifests.
- The deployment targets a real but different application — it exists, so it is not `app not found`, but it does not contain this image.
- The tag was never committed: see [Image tag is never committed](#image-tag-is-never-committed-write-back-skipped).

**How to verify:** compare the requested image against the list in the failure reason, or run `argocd app manifests <app> | grep image:`.

**When the check does not run:** it needs a freshly reconciled application, so it is skipped when `ARGO_REFRESH_APP` is `false` or the task sets `TASK_REFRESH=false`.

**When the check is wrong:** two kinds of image belong to an application yet never appear in the desired state Argo CD reports, so a correct name still fails:

- **Images used only by a sync hook** — Argo CD omits hook resources, so an image appearing only in a PreSync migration Job is invisible.
- **Images named by a custom resource** — if an operator creates the workload from a CR, that workload is not a managed resource of the application.

Turn the check off for such an application. Its deployments then poll for the image and fail only on the timeout, exactly as before:

```yaml
metadata:
  annotations:
    argo-watcher/skip-image-validation: "true"
```

## Tasks stay "in progress" after a server restart

**Symptom:** after a rolling restart or eviction, tasks that were deploying stay `in progress` and are later marked `aborted`, even though Argo CD shows the deployment succeeded.

**Cause with `STATE_TYPE=postgres`:** normally none — a deployment whose replica stops is claimed by another one and monitored through to its real outcome, within about 15 seconds of a graceful shutdown or 45 seconds of a crash. See [High Availability](high-availability.md#task-ownership).

A task that still ends up `aborted` means its rollout window elapsed while no replica was watching it. The window is measured from when the deployment was accepted and is not restarted by a handover, so a restart late in a long rollout can exhaust it. The recorded reason names this case specifically:
`Deployment window elapsed while the task was unattended; marked aborted by argo-watcher.`

The other possibility is that no replica was left to take it over: with a single replica, nothing claims the task until that pod is back.

**Cause with `STATE_TYPE=in-memory`:** the task lives only in the process that accepted it, so it cannot be handed over at all. The row is later reaped by the obsolete-task sweep, which marks a row `aborted` once it has gone quiet for the task's own rollout window plus an hour (its `TASK_TIMEOUT`, or the server's `DEPLOYMENT_TIMEOUT`) — bookkeeping only, so the recorded status can disagree with what really happened. This is expected, and is **not** fixed by tuning the shutdown budget: a graceful shutdown protects the git commit, not the task status. Treat the GitOps repository and Argo CD as the record of what happened, or move to Postgres.

**How to verify**

```bash
# Which replica owns the task, and when its claim lapses
psql -c "SELECT id, status, owner_id, lease_expires_at FROM tasks WHERE status = 'in progress'"

# The takeover, on the replica that picked the deployment up
kubectl logs <pod> | grep -i "abandoned by another replica"

kubectl logs <pod> --previous | grep -i shutdown
```

An `owner_id` of `NULL` on an in-progress task means it is waiting to be claimed; a `lease_expires_at` in the past means the same. Either should resolve within one sweep.

`git write-back batch drain did not finish before the shutdown deadline` means queued commits were abandoned mid-flush — that part *is* tunable.

**Fix**

1. Check the GitOps repository for the commit. A resumed deployment re-runs the write-back, and skips it when the tag is already committed, so a missing commit means no replica ever got that far: re-run the pipeline job.
2. Graceful shutdown needs up to 25s: raise `terminationGracePeriodSeconds` if it is below the Kubernetes default of 30s, and keep `GIT_OP_TIMEOUT` under the shutdown budget so an in-flight attempt can finish.
3. If tasks are consistently aborted with the unattended-window reason, the rollout window is too tight for how long your pods take to be replaced. Raise `DEPLOYMENT_TIMEOUT`, or run at least two replicas so a handover does not wait for a restart.

## Web UI is not accessible

**Symptom:** 404, connection refused, or a certificate error.

**How to verify**

```bash
kubectl describe ingress argo-watcher
echo | openssl s_client -servername <domain> -connect <host>:443
curl -s $ARGO_WATCHER_URL/livez
```

**Fix**

1. Confirm the Ingress has an address and points at the service and its container port.
2. Confirm the TLS secret exists and matches the host.
3. Confirm `STATIC_FILES_PATH` points at the built UI assets (`/app/static` in the published image). A server with no assets there answers `404` for every UI path while the API keeps working.

## Web UI or Swagger page loads blank

**Symptom:** the page returns `200` with an HTML body, but nothing renders. The API answers normally.

**Likely cause:** a [Content-Security-Policy](../reference/api.md#security-headers) is blocking an asset. The server sends its own policy, and a proxy that adds a second one does not replace it — the browser enforces both, so a directive either policy omits blocks the load.

**How to verify:** open the browser console. A blocked asset is reported as a policy violation naming the directive that refused it. Compare the header the browser received with the one the server sent:

```bash
curl -sI $ARGO_WATCHER_URL/ | grep -i content-security-policy
```

**Fix:** stop the proxy adding a policy of its own, or widen it to cover the same sources. Do not strip the server's policy at the proxy — it is the only one the published image guarantees.

## Web UI stops on "Sign-in failed"

**Symptom:** with [OIDC](../guides/oidc.md) enabled, the loading screen shows a red error box under the logo and never reaches the task list. It does not return to the provider on its own.

**The box names the cause:**

- *Could not start the sign-in* — the browser never reached the provider, almost always because it could not read `<issuer>/.well-known/openid-configuration`. Usually `OIDC_ISSUER_URL` is an address only the server can resolve (an in-cluster Service name), or the provider does not answer that document cross-origin. The second line carries the browser's own message.
- *The identity provider rejected the sign-in* — the provider returned an OAuth error; the box shows its code (`invalid_scope`, `access_denied`, `invalid_client`, …).
- *The sign-in response could not be completed* — the response came back but could not be matched to the sign-in that started: storage cleared mid-flow, a callback URL reopened from history, or too much clock skew.
- *Argo Watcher is misconfigured for OIDC* — the server reports OIDC as enabled without an issuer or a client id.

**How to verify**

- Open the discovery URL from the box in the same browser. It must return JSON; a DNS error, TLS warning, or CORS failure in the console is the cause.
- Check the server's view: `curl -s $ARGO_WATCHER_URL/api/v1/config | jq .oidc`.

**Fix**

1. Set `OIDC_ISSUER_URL` to the issuer as users' browsers reach it — both browser and server must be able to use it.
2. Correct whatever the error code points at: permitted scopes (`openid profile email`), exact redirect URI, web origin, group or user assignment. See [Redirect URI and web origin](../guides/oidc.md#redirect-uri-and-web-origin).
3. Press **Try again**. Nothing is cached across the retry.

## Signed-in users are sent back to the provider periodically

**Symptom:** with [OIDC](../guides/oidc.md) enabled, a session that should renew itself quietly instead bounces through the provider's login page, and the browser console reports a `frame-src` policy violation.

**Likely cause:** the renewal falls back to an iframe when the provider issues no refresh token, and that iframe navigates to the authorization endpoint. [`frame-src`](../reference/api.md#security-headers) allows the `OIDC_ISSUER_URL` origin only, so a provider that serves the endpoint from a different host — Amazon Cognito uses the managed-login domain — cannot renew this way.

**Fix:** nothing to configure. Sign-in still works and the user keeps their session; only the silent part of the renewal is lost. Configure the provider to issue refresh tokens if the redirect is disruptive.

## Web UI does not update without a refresh

**Symptom:** task status and deploy-lock changes appear only after a manual page reload.

**Likely cause:** a proxy in front of the server strips the `Upgrade` and `Connection` headers, so the WebSocket handshake on `/ws` never happens and the server answers `400`.

With [OIDC](../guides/oidc.md#the-websocket-handshake) enabled there is a second candidate: a proxy that forwards the upgrade but strips `Sec-WebSocket-Protocol` removes the credential the browser sends there, and the handshake is refused `401`.

A third candidate is a proxy that rewrites `Host` to its upstream instead of preserving the client's: the [`Content-Security-Policy`](../reference/api.md#security-headers) then names the wrong `wss://` origin and the browser refuses the connection before it is made. The console reports it as a policy violation, and the server logs nothing at all.

**How to verify:** with `LOG_LEVEL=debug`, look for `non-upgrade request to /ws` — it logs the `upgrade` and `connection` values that actually arrived. A `rejecting unauthenticated websocket` warning while REST reads work in the same browser points at a stripped subprotocol rather than a dead session.

**Fix**

1. Enable WebSocket support on the proxy. For nginx:

    ```nginx
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    ```

2. On an nginx Ingress, make sure the read timeout is long enough to hold the connection open (`nginx.ingress.kubernetes.io/proxy-read-timeout`).

## All deployments rejected as locked, but nobody set a lock

**Symptom:** every deployment is rejected with `406` and `lockdown is active, deployments are not accepted`, with no manual lock set and no scheduled window open.

**Cause:** with `STATE_TYPE=postgres` the lock lives in the database. If it cannot be read the server fails closed and treats the unknown state as locked. Two different problems do that: the database is unreachable, or the `deploy_lock` table does not exist because migration `000006` was never applied.

**How to verify**

- `GET /api/v1/deploy-lock` returns `true`.
- The log contains `failed to read deploy lock state, assuming locked`. The error on that line tells the two apart: a connection error for an outage, `relation "deploy_lock" does not exist` for a missing migration.
- `/api/v1/reachability` reports the `database` reason only for a genuine outage. A healthy reachability response alongside a permanent lock points at the migration.

**Fix:** restore connectivity, or [apply the migrations](database.md#migrations). Lock resolution returns to normal on the next successful read.

## Lock will not release

**Symptom:** deployments stay blocked after releasing the [deploy lock](../guides/deployment-lock.md).

**Likely causes**

- A [scheduled window](../guides/deployment-lock.md#scheduled-lockdown) is open. Releasing inside a window only suppresses it for 15 minutes.
- The `DELETE` never reached the server: those endpoints exist only when OIDC is enabled, and require a privileged group.
- The lock state cannot be read at all — see [the entry above](#all-deployments-rejected-as-locked-but-nobody-set-a-lock).

**How to verify**

```bash
curl -i -X DELETE -H "Oidc-Authorization: $OIDC_TOKEN" \
  $ARGO_WATCHER_URL/api/v1/deploy-lock
```

- `200` with `Content-Type: text/html` means OIDC is disabled, so the endpoint does not exist and the Web UI answered instead.
- `401` means the token was rejected or the user is not in a privileged group.
- `500` means the state could not be persisted; the reason is in the server log.

Also check whether `LOCKDOWN_SCHEDULE` covers the current time. Schedules are evaluated in the server's timezone, which is UTC in the published image unless `TZ` is set.

**Fix:** re-issue the `DELETE` with a privileged token, or remove the window from `LOCKDOWN_SCHEDULE` — a release cannot cancel a schedule, only suppress it for 15 minutes at a time.

## Webhook not firing

**Symptom:** no notifications arrive for deployments.

**Likely causes**

- `WEBHOOK_ENABLED` is not `true`, or `WEBHOOK_URL` is unset or unreachable from the server.
- The receiver answers with a code that is not in `WEBHOOK_ALLOWED_RESPONSE_CODES` (default `200` only). A receiver replying `201` or `204` counts as a failure until you list it.
- The receiver rejects the request because `WEBHOOK_AUTHORIZATION_HEADER_NAME`/`_VALUE` do not match what it expects, or `WEBHOOK_CONTENT_TYPE` does not match the body.
- `WEBHOOK_FORMAT` references a field that does not exist. (A template that does not *parse* fails startup instead, so the server would not be running.)

**How to verify**

- Look for `Failed to dispatch notification` in the server log — it carries the response code and the receiver's body. `LOG_LEVEL=debug` additionally logs the rendered payload.
- Reproduce the call by hand:

    ```bash
    curl -i -X POST "$WEBHOOK_URL" \
      -H "Content-Type: application/json" \
      -H "Authorization: $WEBHOOK_AUTHORIZATION_HEADER_VALUE" \
      -d '{"app":"demo","status":"deployed"}'
    ```

**Fix**

1. Add every success code your receiver returns to `WEBHOOK_ALLOWED_RESPONSE_CODES`.
2. Check the template fields against [Template variables](../guides/notifications.md#template-variables).
3. Remember Argo Watcher does not sign the payload — the authorization header is the only credential it sends, so a receiver expecting a signature will always reject it.
