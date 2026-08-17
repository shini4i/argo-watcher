# Troubleshooting

Each entry follows the same shape: **Symptom · Likely cause · How to verify · Fix.** When a symptom shows up in monitoring first, [Observability](observability.md) tells you which metric to look at; this page tells you what to do once you have.


## Server does not start

**Symptom:** The Argo Watcher server pod fails to start or repeatedly crashes.

**Likely causes:**
- `ARGO_URL` or `ARGO_TOKEN` are incorrect or missing.
- The server cannot reach the Argo CD API.
- If using `STATE_TYPE=postgres`, the database is not accessible or migrations have not been applied. The server (and `--migrate`) fail fast within `DB_CONNECT_TIMEOUT` seconds (default 10) instead of hanging on the OS TCP timeout; check the logs right after the `Connecting to PostgreSQL database...` line for the connection error.

**How to verify:**
- Check the server logs: `kubectl logs -f <pod-name>` or `docker logs <container-name>`.
- Set `LOG_LEVEL=debug` for verbose output.
- Verify connectivity: `curl -H "Authorization: Bearer $ARGO_TOKEN" $ARGO_URL/api/v1/applications`.

**Fix:**
1. Confirm that `ARGO_URL` is the correct Argo CD API URL (e.g., `https://argocd.example.com`).
2. Generate a new token if the existing one is expired or invalid.
3. If using Postgres, run migrations: `goose -dir ./migrations postgres "$DATABASE_URL" up`.

---

## Client exits with a non-zero code

**Symptom:** The Argo Watcher client in a CI/CD pipeline returns a non-zero exit code, failing the build.

**Likely causes:**
- The `ARGO_WATCHER_URL` is not reachable from the CI runner.
- The `ARGO_APP` name does not match an application in Argo CD.
- The `IMAGES` or `IMAGE_TAG` do not correspond to the built image.
- The client timed out waiting for the deployment to complete.
- Argo CD (or the state backend) is unreachable — the server now fails the submission fast with a `503` `{"status":"down"}` response instead of hanging until the client's own HTTP timeout, and the Web UI shows an "unreachable" banner naming the affected subsystem (ArgoCD, the state backend, or both) until connectivity is restored.
- Argo CD became unreachable *during* the deployment check (timeout, DNS/TLS error, connection refused, or a `5xx` response). The task is recorded as `aborted` and the client logs `The deployment was aborted before its outcome could be confirmed. See the reason below.` followed by the task's status reason (e.g. `ArgoCD API Error: ...`). The application itself may be healthy — check Argo CD directly before blaming the deployment.
- A newer deployment of one of the same images was submitted while this one was still in progress, so the server cancelled this task (client logs `The deployment was cancelled because a newer deployment superseded it`). Only tasks sharing an image with the newer deployment are cancelled; independent per-image deployments of the same application do not cancel each other. A deployment also only cancels one that presented no more authority than itself, so an uncredentialed deployment never cancels a credentialed one still in flight. This is by design, not a real failure — confirm the newer deployment succeeded; no other action is needed.

**How to verify:**
- Check the client logs in the CI output.
- Manually test reachability: `curl $ARGO_WATCHER_URL/readyz` from the CI runner.
- Verify the app name: `argocd app get <ARGO_APP>`.
- Check the image that was pushed to the registry.

> **Note:** While polling for deployment status, the client automatically retries transient failures (network errors or `5xx` responses) up to 3 times with a 2-second backoff, so a single blip is not fatal. A non-zero exit here means the failure persisted past those retries or was terminal (`4xx`, invalid token).

**Fix:**
1. Ensure `ARGO_WATCHER_URL` is accessible from the CI environment (check firewall rules and DNS).
2. Verify the application name in `ARGO_APP` matches Argo CD exactly (case-sensitive).
3. Confirm that `IMAGES` and `IMAGE_TAG` match the tag that was pushed.
4. Increase `DEPLOYMENT_TIMEOUT` if deployments consistently take longer than 900 seconds.

---

## Deployment times out

**Symptom:** The client reports "deployment timed out" even though the application is deploying correctly.

**Likely causes:**
- The default `DEPLOYMENT_TIMEOUT` (900 seconds / 15 minutes) is too short for the workload.
- Argo CD is not detecting the image update.
- The requested image is not part of the application at all. This normally fails immediately rather than timing out — see [Image is not part of application](#image-is-not-part-of-application) for the cases where it cannot be detected.
- When relying on the built-in GitOps updater: the task was submitted without a valid deploy token or JWT, so the write-back was silently skipped and Argo CD never received the new tag. (If image tags are committed by other means — Argo CD Image Updater, your CI — this is expected and not the cause.)

**How to verify:**
- Check the Argo CD UI to confirm the application is syncing and the new image is being deployed.
- Verify that the image tag annotation was correctly set: `kubectl describe app <ARGO_APP> -o yaml | grep -A5 argo-watcher`.
- Confirm the CI job actually supplied `ARGO_WATCHER_DEPLOY_TOKEN` or `BEARER_TOKEN`. For an application annotated `argo-watcher/managed: "true"` the server logs a warning — "application is managed by the watcher but the task presented no valid credential" — and increments `gitops_writeback_skipped_unvalidated`; see [Image tag is never committed](#image-tag-is-never-committed-write-back-skipped).

**Fix:**
1. Increase `DEPLOYMENT_TIMEOUT` to accommodate your typical rollout duration.
2. If using the built-in GitOps updater, verify the SSH key has write access to the target repository.
3. Check Argo CD logs for sync errors: `kubectl logs -l app.kubernetes.io/name=argocd-application-controller`.

---

## Image tag is never committed (write-back skipped)

**Symptom:** The server logs `Skipping git repo update: application is managed by the
watcher but the task presented no valid credential` and
`gitops_writeback_skipped_unvalidated` rises for that application.

Write-back requires a credential on `POST /api/v1/tasks`. Without one the task is still
accepted and monitored — submission is deliberately open — but the new tag is never
committed, so what the pipeline reports next does not name the real cause:

| Application | What the deployment reports |
|---|---|
| Normal | Fails as `Image "<name>" is not part of application "<app>"` — the image is genuinely absent, because the commit that would have added it never happened. |
| Normal, with image validation off (`ARGO_REFRESH_APP=false`, `refresh: false`, or `argo-watcher/skip-image-validation`) | Waits out `DEPLOYMENT_TIMEOUT`, then fails. |
| `argo-watcher/fire-and-forget: "true"` | **Reports success.** The rollout is never checked, so nothing contradicts it. |
| Redeploy of a tag the application already runs | Reports success, correctly — there was nothing to commit. |

So a failure here usually blames the image or the clock. The server-side warning and
counter above are what identify the credential as the cause.

**Likely causes:**
- The pipeline sets neither `ARGO_WATCHER_DEPLOY_TOKEN` nor `BEARER_TOKEN`.
- The value does not match the server's `ARGO_WATCHER_DEPLOY_TOKEN` / `JWT_SECRET`.
- **Something between the client and the server redirects to a different host.** A
  credential is generally not carried across a host change, so it never reaches the server.
  The two credentials behave differently:
    - `Authorization` (`BEARER_TOKEN`) is stripped by Go's HTTP client on **every** client
      version — but only when the *hostname* changes. Go compares hostnames with the port
      excluded, so it is preserved across a port-only change (`host:8080` → `host:9090`) and
      when the target is a subdomain of the original (`example.com` → `sub.example.com`).
      A sibling hostname is not a subdomain: `watcher.example.com` →
      `watcher.int.example.com` drops it.
    - `ARGO_WATCHER_DEPLOY_TOKEN` is a custom header, which Go always forwards, so the
      client deletes it itself on any change of host **or port** — from v0.15.0 onwards.
      Earlier clients forwarded it, so this can appear on a client upgrade with nothing
      else changing.

  Clients from this release onward log a warning naming both hosts, once per run, whenever
  a credential does not survive the hop.

**How to verify:**
```bash
curl -sSI "$ARGO_WATCHER_URL/api/v1/config"
```
A `200` means there is no redirect. A `301`, `302`, `307` or `308` with a `Location`
pointing at a different hostname is the cause. (A `301`/`302` on the API path breaks
submission outright, since Go rewrites the `POST` to a `GET`; a `307`/`308` preserves the
request and loses only the credential.)

**Fix:**
1. Point `ARGO_WATCHER_URL` at the hostname that actually serves the API.
2. If the pipelines cannot be changed, serve both hostnames from the same ingress instead
   of redirecting one to the other — then there is no host change. Redirecting browser
   navigation while passing `/api/v1/*` and `/ws` through keeps the canonical URL in the
   address bar.

Do not work around this by allowing the credential to follow the redirect. The deploy
token does not expire, is not scoped to an application, and authorizes commits to the
GitOps repository — whoever answers for the redirect target receives it on every request.

---

## Client refuses a redirect away from https

**Symptom:** The deployment fails immediately with
`refused to follow a redirect away from https`, naming the endpoint that answered and the
plain-`http` target it pointed at.

**Meaning:** `ARGO_WATCHER_URL` is an `https` URL, but something on the path answers it
with a `Location` on plain `http`. Go's HTTP client carries `Authorization` across such a
redirect as long as the hostname is unchanged, so following it would put the deploy token
or CI JWT on the wire in the clear. The client refuses instead, and does not retry — the
redirect is a property of the configuration, not a transient failure.

**Fix:** Point `ARGO_WATCHER_URL` at the endpoint that serves the API over TLS, or stop
the ingress in front of it from downgrading API requests. Only stepping down from TLS is
refused — a client deliberately pointed at an `http://` URL keeps working, unless the
chain has already reached `https` by the time it is redirected back.

---

## Image is not part of application

**Symptom:** The deployment fails immediately with `Image "<name>" is not part of application "<app>"`, followed by the list of images the application defines.

**Meaning:** The application finished rolling out — synced and healthy — but its desired state does not declare the requested image at all, so waiting for that image to appear would only burn the task's timeout. Argo Watcher reads the desired state from Argo CD's managed resources, not from the running pods, so a workload that has no pod yet (an untriggered `CronJob`, a `Deployment` scaled to zero) still counts as declaring its image.

**Likely causes:**
- The image name is misspelled, or the registry prefix does not match what the manifests use.
- The deployment targets a real but different application — one that exists, so it is not reported as `app not found`, but does not contain this image.

**How to verify:** Compare the requested image with the list in the failure reason, or run `argocd app manifests <app> | grep image:`.

**Fix:** Correct the image name, or the `ARGO_APP`, in the deployment request.

**When the check does not run:** it needs a freshly reconciled application, so it is skipped entirely when `ARGO_REFRESH_APP` is `false` or the task sets `refresh: false`. Without a refresh the desired state may be older than the deployment, and a legitimate image could look absent.

**When the check is wrong:** two kinds of image are part of an application but never appear in the desired state Argo CD reports, so deploying them would hit this failure even though the image name is correct:

- **Images used only by a sync hook.** Argo CD omits hook resources from its managed-resources response, so an image that appears only in, say, a PreSync migration Job is invisible.
- **Images named by a custom resource.** If the application manages a CR and an operator creates the actual workload from it, that workload is not a managed resource of the application, and the CR usually names the image in a field of its own rather than in a pod template.

Annotate such an application to turn the check off for it. Its deployments then behave exactly as before: they keep polling for the image and fail only when the timeout expires.

```yaml
metadata:
  annotations:
    argo-watcher/skip-image-validation: "true"
```

---

## Web UI is not accessible

**Symptom:** The Argo Watcher Web UI returns a 404, connection refused, or certificate error.

**Likely causes:**
- The Ingress resource is not configured correctly.
- TLS certificate is missing or invalid.
- Static UI files are not in the expected location.

**How to verify:**
- Check Ingress status: `kubectl describe ingress argo-watcher`.
- Verify the Ingress rule points to the correct service and port.
- Check that the TLS certificate is valid: `echo | openssl s_client -servername <domain> -connect <host>:443`.

**Fix:**
1. Ensure the Ingress resource is created and its status shows a valid IP/hostname.
2. Verify the TLS certificate is installed and references the correct secret.
3. Check that `STATIC_FILES_PATH` in the Argo Watcher server config points to the directory containing the built UI assets (typically `/app/static` in Docker).
4. Restart the server pod to pick up any configuration changes.

---

## Web UI stops on "Sign-in failed"

**Symptom:** With [OIDC](../guides/oidc.md) enabled, the Web UI shows its loading screen with a red error box beneath the logo and never reaches the task list. It does not return to the identity provider on its own.

**Likely causes:** the box names the one that applies.

- *Could not start the sign-in* — the browser never got as far as the provider, almost always because it could not read the discovery document at `<issuer>/.well-known/openid-configuration`. Most often `OIDC_ISSUER_URL` is an address only the server can resolve (an in-cluster Service name), or the provider does not answer that document cross-origin. The box's second line carries the browser's own message, which also covers the rarer case of browser storage refusing the sign-in state.
- *The identity provider rejected the sign-in* — the provider answered with an OAuth error. The box shows the provider's own code (`invalid_scope`, `access_denied`, `invalid_client`, …) and description.
- *The sign-in response could not be completed* — the response came back but the browser could not match the sign-in it started: browser storage was cleared mid-flow, a callback URL was reopened from history or a bookmark, or the clock skew between browser and provider is too large.
- *Argo Watcher is misconfigured for OIDC* — the server reports OIDC as enabled without an issuer URL or a client id.

**How to verify:**

- Open the discovery URL named in the box in the same browser. It must return JSON; a DNS error, a TLS warning, or a CORS failure in the browser console is the cause.
- For a rejected sign-in, look up the shown code in your provider's documentation, and check the client registration against [Redirect URI and web origin](../guides/oidc.md#redirect-uri-and-web-origin).
- Check the server's OIDC settings: `curl -s $ARGO_WATCHER_URL/api/v1/config | jq .oidc`.

**Fix:**

1. Set `OIDC_ISSUER_URL` to the issuer as users' browsers reach it, not as the pod reaches it. Both must work: the browser performs discovery and the code exchange, and the server validates tokens against the same issuer.
2. Correct the client registration the shown error code points at — permitted scopes (`openid profile email`), exact redirect URI, web origin, and user or group assignment.
3. Press **Try again** in the error box once the provider is fixed. Nothing is cached across the retry; it re-runs the whole sign-in.

---

## Web UI does not update without a refresh

**Symptom:** The Web UI loads, but task status and deploy-lock changes only appear after a manual page refresh.

**Likely cause:** A reverse proxy or load balancer in front of the server is stripping the `Upgrade` and `Connection` headers, so the WebSocket handshake on `/ws` never happens and the server answers 400.

With [OIDC](../guides/oidc.md#the-websocket-handshake) enabled there is a second candidate: a proxy that forwards the upgrade but strips `Sec-WebSocket-Protocol` removes the credential the browser sends there, and the handshake is refused with 401.

**How to verify:**
- Set `LOG_LEVEL=debug` and look for `non-upgrade request to /ws`. It logs the `upgrade` and `connection` header values that actually arrived — an empty `upgrade` means the proxy dropped it before the request reached Argo Watcher.
- A `rejecting unauthenticated websocket` warning in the server log, while the REST reads work in the same browser, points at a stripped `Sec-WebSocket-Protocol` rather than a session problem.

**Fix:**
1. Enable WebSocket support on the proxy. For nginx, forward the headers explicitly:
   ```nginx
   proxy_set_header Upgrade $http_upgrade;
   proxy_set_header Connection "upgrade";
   ```
2. On an nginx Ingress, confirm the read/send timeouts are long enough to keep the connection open (`nginx.ingress.kubernetes.io/proxy-read-timeout`).

---

## All deployments rejected as locked, but nobody set a lock

**Symptom:** Every deployment is rejected with `406` and `lockdown is active, deployments are not accepted`, while no manual lock was set and no scheduled window is open.

**Likely causes:** With `STATE_TYPE=postgres` the lock state lives in the database. If it cannot be read, the server fails closed and treats the unknown state as locked. Two different problems produce that:

- The database is unreachable.
- The `deploy_lock` table does not exist because migration `000006` has not been applied — the database itself is perfectly healthy.

**How to verify:**
- `GET /api/v1/deploy-lock` returns `true`.
- The server log contains `failed to read deploy lock state, assuming locked`. The error on that line tells the two causes apart: a connection error for an outage, `relation "deploy_lock" does not exist` for a missing migration.
- `/reachability` reports the `database` reason only for a genuine outage. A healthy `/reachability` alongside a permanent lock points at the migration.

**Fix:** Restore database connectivity, or [apply the migrations](database.md#migrations). Lock resolution returns to normal on the next successful read; no manual intervention on the lock itself is needed.

---

## Lock will not release

**Symptom:** Deployments stay blocked after releasing the deploy lock. The lock is server-global — there is no per-application lock.

**Likely causes:**
- A [scheduled lockdown](../guides/gitops-updater.md#scheduled-lockdown) window is open. Releasing the lock during a window only suppresses it for 15 minutes, after which it takes effect again.
- The `DELETE` never reached the server: the state-changing endpoints are only registered when OIDC is enabled (otherwise the request falls through to the Web UI and answers `200` with HTML), and they require a token from a user in one of the `OIDC_PRIVILEGED_GROUPS` (otherwise `401`).
- The lock state cannot be read at all — see [All deployments rejected as locked, but nobody set a lock](#all-deployments-rejected-as-locked-but-nobody-set-a-lock).

**How to verify:**
- Read the current state: `curl -H "Oidc-Authorization: $OIDC_TOKEN" $ARGO_WATCHER_URL/api/v1/deploy-lock` (with OIDC enabled any credential works and no privileged group is needed; with OIDC disabled the header can be omitted).
- Check the response code of the release itself:
  ```bash
  curl -i -X DELETE -H "Oidc-Authorization: $OIDC_TOKEN" $ARGO_WATCHER_URL/api/v1/deploy-lock
  ```
  A `200` carrying HTML (`Content-Type: text/html`) rather than JSON means OIDC is disabled, so the endpoint does not exist and the Web UI answered instead. `401` means the token was rejected or the user is not in a privileged group, and `500` means the lock state could not be persisted (the reason is in the server log).
- Check whether `LOCKDOWN_SCHEDULE` covers the current time. The server evaluates schedules in its own timezone, which is UTC in the published container image unless `TZ` is set.

**Fix:**
1. Re-issue the `DELETE` with a token belonging to a privileged group.
2. If a scheduled window is the cause, either wait for it to close or remove the window from `LOCKDOWN_SCHEDULE` — a release cannot cancel a schedule, only suppress it for 15 minutes at a time.

---

## Webhook not firing

**Symptom:** Deployment status notifications are not being sent to the configured webhook.

**Likely causes:**
- The webhook URL is incorrect or unreachable.
- The webhook signature does not match the configured secret.
- Template variable syntax is incorrect.
- The webhook endpoint is not accepting POST requests.

**How to verify:**
- Check the server logs for webhook delivery errors: `LOG_LEVEL=debug`.
- Verify the webhook URL is reachable: `curl -X POST <webhook_url>`.
- Test the signature: Argo Watcher uses SHA256 HMAC of the payload body with the secret.

**Fix:**
1. Verify the webhook URL is correct and accessible from the Argo Watcher server.
2. Test the webhook payload locally before deploying.
3. Check that all template variables are valid — refer to [Webhook Template Variables](../guides/notifications.md#available-template-variables).
4. Ensure the webhook endpoint is configured to accept POST requests with JSON payloads.

---

## Tasks stay "in progress" after a server restart

**Symptom:** After a rolling restart or pod eviction, tasks that were deploying remain "in progress" and are only later marked "aborted", even though Argo CD shows the deployment succeeded.

**Cause:** A deployment being tracked when the process exits is not handed off to another replica, and nothing waits for the tracker to persist a final status before the process terminates. The task row is later reaped by the obsolete-task sweep, which marks "in progress" rows older than one hour as "aborted" — bookkeeping only, so the recorded status can disagree with what actually happened in Argo CD.

This is expected on any restart and is **not** fixed by tuning the shutdown budget. A graceful shutdown protects the git commit and stops write-backs retrying past the deadline; it does not make the task's final status durable. Until task ownership survives a pod replacement, treat the GitOps repository and Argo CD as the record of what happened, not the task status.

**How to verify:**
- Check the shutdown logs of the previous pod: `kubectl logs <pod> --previous | grep -i shutdown`.
- The warning `git write-back batch drain did not finish before the shutdown deadline` means queued git commits were abandoned mid-flush — that part *is* tunable, below.

**Fix:**
1. Confirm in your GitOps repository whether the image tag was committed. If it was not, re-run the pipeline job — an interrupted write-back is not resumed automatically.
2. To stop losing the commit as well: graceful shutdown needs up to 25s, so raise `terminationGracePeriodSeconds` if it is below the Kubernetes default of 30s, and keep `GIT_OP_TIMEOUT` under the shutdown budget so an in-flight attempt can finish during the drain.

## Task is stuck in "pending" state

**Symptom:** A task created in Argo Watcher is stuck and does not transition to "in progress" or "failed".

**Likely causes:**
- The expected image has not been updated in Argo CD yet.
- Argo CD API is unreachable or returning stale data.
- A deployment lock is preventing status updates.

**How to verify:**
- Check the Argo CD UI to confirm the image has been updated.
- Verify the server can reach the Argo CD API: `kubectl exec -it <pod> -- curl -H "Authorization: Bearer $ARGO_TOKEN" $ARGO_URL/api/v1/applications/<app>`.
- Check if the application is locked.

**Fix:**
1. Verify that the image tag was correctly updated in your GitOps repository.
2. Manually refresh the application in Argo CD: `argocd app get <app> --refresh`.
3. Increase `ARGO_API_TIMEOUT` if the Argo CD API is slow to respond.
4. Restart the server pod to clear any cached state.
