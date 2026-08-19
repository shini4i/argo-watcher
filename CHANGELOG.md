# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Argo Watcher can now run with more than one replica when `STATE_TYPE=postgres`. Each
  in-progress deployment is owned by exactly one replica, recorded as a lease on the task
  row. A replica that stops mid-rollout — crash, eviction, or a rolling update — has its
  deployments claimed and finished by another replica instead of leaving them stuck in
  progress until the hourly stale sweep marked them aborted. Takeover happens within about
  15 seconds of a graceful shutdown, or 45 seconds of a crash.

  A resumed deployment keeps the window it was accepted with, measured from its creation,
  so passing between replicas cannot extend how long it polls; one whose window elapsed
  while unattended is recorded as `aborted` with a reason naming that cause. There are no
  new environment variables and no API changes, and `STATE_TYPE=in-memory` is unaffected —
  it remains single-replica only. The new High Availability page in the documentation
  covers what is shared between replicas and what is not.

  Note for the upgrade itself: deployments already in flight when the first new replica
  starts carry no lease, so for the duration of the rollout one of them may briefly be
  watched by both an old and a new replica and reported twice. No duplicate commit results
  — a write-back whose tag is already committed is skipped.

- A new `gitops_writeback_skipped_unvalidated` counter (labelled by `app`) reports
  deployments of an application annotated `argo-watcher/managed: "true"` whose task
  presented no valid credential, so the image tag was never committed. Any non-zero value
  is a misconfiguration, and it is the only signal that names the cause: the deployment
  that follows fails as `Image "<name>" is not part of application "<app>"` — the image
  really is absent, because the commit that would have added it never happened — or waits
  out `DEPLOYMENT_TIMEOUT` when image validation is off. Under
  `argo-watcher/fire-and-forget` it reports success instead. The matching server log moved
  from debug to warn and now names the application; an application that is simply not
  managed by the watcher still logs at debug, since skipping it is routine.

  The usual cause is a credential dropped in transit by a redirect that leaves the host
  the client was pointed at. A client from this release on also logs one warning naming
  both hosts when that happens, rather than dropping the credential silently.

### Changed

- The HTTP server now routes with `chi` instead of `gin`. Endpoints, status codes,
  response bodies and metric labels are unchanged, verified request by request against
  the previous implementation — including the `/swagger` mount, the trailing-slash
  redirect, the CORS origin rules and the `path="/api/v1/tasks/:id"` label on
  `unauthenticated_reads`. Two deliberate differences: preflight replies now echo the
  requested method and headers rather than the full configured lists, and a cross-origin
  request to a URL that only differs by a trailing slash is now refused by the origin
  check instead of being redirected before it. The set of origins, methods and headers
  actually permitted is the same, and both differences only affect requests that were
  already going to be rejected or are never sent by a browser.

  Twenty transitive dependencies pulled in only by the previous framework are gone with
  it, which takes about 15 MB off the binary.
- Database migrations now connect through the same PostgreSQL driver the server itself
  uses (`pgx`), instead of a second driver linked only for migrations. The `DB_*`
  variables, the migrations and the advisory lock that serializes them are unchanged, as
  is running migrations with the external `migrate` CLI.
- A deployment that fails while the application is still out of sync now leads with what
  Argo CD reports and how long the rollout was waited on — `Deployment failed: ArgoCD
  reports sync status OutOfSync after waiting 3m45s.` — and answers the obvious question
  next. When the revision the last sync applied differs from the one Argo CD now compares
  against, the report names both and says the desired state has changed since that sync;
  when they match, it says applying it did not converge the live state and lists the usual
  causes. Either way it states whether auto-sync is enabled, which decides whether anyone
  has to act. The last sync operation, which routinely reads `Succeeded` / `successfully
  synced (all tasks run)`, now closes the report as context instead of opening it and
  reading as a contradiction. Resources that only reach `Synced` by being deleted are
  flagged `(requires pruning)`, and a report with none of this still names the last sync
  operation rather than being blank.
- Failure reports no longer list resources that applied cleanly. Argo CD reports a
  successful apply with the hook phase `Running` and kubectl's own `configured` message, so
  those resources used to appear as `Deployment(app)  Running with message
  deployment.apps/app configured` in a list of things that had gone wrong — the single most
  misleading line in the report. Only resources that actually failed are listed, the
  section is now headed `Failed resources:` rather than `Failed hooks:` (a failed apply is
  not a hook), and each line names the outcome that describes it instead of always the hook
  phase, so a resource whose live object went unhealthy mid-sync no longer reads `Synced`.

### Fixed

- A sign-in that cannot be completed — a provider error, a callback that cannot be
  exchanged, a redirect that never starts, or a server reporting OIDC as enabled without an
  issuer or client id — now stops on the Web UI's loading screen and states why, instead of
  rendering a signed-out UI whose only explanation was a line in the browser console. The
  screen shows the provider's own error code and description, or the discovery URL the
  browser could not read — most often an `OIDC_ISSUER_URL` that resolves only inside the
  cluster — and offers a retry rather than heading back to the provider that just failed.
  Deployments without OIDC are untouched, as is a configuration request that merely fails
  while the server restarts.

### Security

- The client now refuses a redirect that moves it from `https` to plain `http`, instead of
  following it with the deploy token or CI JWT attached. Go's HTTP client compares only
  hostnames when deciding whether a credential may cross a redirect, so a `Location` header
  pointing at `http://` on the same host would have put the credential on the wire in the
  clear. Such a deployment now fails immediately naming both endpoints, rather than being
  retried as a network blip. Only stepping down from TLS is refused: a client pointed at an
  `http://` URL keeps working, and so does any redirect that stays on `https`.
- Bumped the Go toolchain to `1.26.6`, resolving six standard library vulnerabilities
  reachable from this code base in `go1.26.5`: `GO-2026-6218` (`net/url`), `GO-2026-6090`
  (`crypto/tls`), `GO-2026-6089` and `GO-2026-5026` (`net/http`), `GO-2026-6088`
  (`encoding/xml`) and `GO-2026-5972` (`encoding/asn1`). The `go` directive stays at
  `1.26.5`, so the minimum language version required to build is unchanged.

## [0.15.0] - 2026-08-11

### Added

- With OIDC authentication enabled, the read endpoints the Web UI consumes —
  `GET /api/v1/tasks`, `/version`, `/reachability` and `GET /deploy-lock` — now
  require a credential. Being signed in is enough; membership of
  `OIDC_PRIVILEGED_GROUPS` remains necessary only for managing the deployment lock.
  A deploy token or a CI JWT is accepted as well, so pipelines can read too.
  Deployments are unaffected: `POST /api/v1/tasks` keeps its optional-credential
  behaviour, and `GET /api/v1/tasks/{id}` — the lookup the client polls — stays open
  by default, guarded by the unguessable task id. With OIDC disabled nothing changes.

  The `/ws` WebSocket, which broadcasts the same deployment-lock and Argo CD
  reachability transitions, requires a credential too. A browser cannot attach a header
  to a WebSocket handshake, so the Web UI passes its token as the
  `argo-watcher.token.<token>` subprotocol; other clients send the usual headers. A
  handshake with no credential is refused with `401` before the connection is upgraded.
- A new `unauthenticated_reads` counter (labelled by `path` and `app`) reports how many
  reads still arrive without a credential on the endpoints left open on purpose, and
  which application's pipeline they belong to. Reads for a task submitted without a
  credential are counted under `app="unknown"`, since an uncredentialed caller must not
  be able to choose a label value. It reaching zero is the evidence needed before those
  can be closed too.
- `OIDC_REQUIRE_TASK_READ_AUTH` closes that last open read: with it set,
  `GET /api/v1/tasks/{id}` requires a credential like every other read. It is opt-in
  because it fails every deployment driven by a client that polls without one — flip
  it once `unauthenticated_reads` has stayed at zero for a full deployment cycle. It
  requires `OIDC_ENABLED=true`; the server refuses to start otherwise, rather than
  accept a setting that could not take effect.

### Changed

- `GET /healthz` is replaced by two endpoints with distinct meanings: `GET /livez`,
  which reports only that the process is still serving and checks no dependency, and
  `GET /readyz`, which reports whether this instance should receive traffic — down
  while shutting down, and down when the state backend is unreachable. Both answer
  `{"status":"up"}` or `503 {"status":"down","reason":"..."}`. The Helm chart points
  each probe at the right endpoint, so upgrading needs no action. The practical gain is
  that a database outage no longer fails the liveness probe: a restart cannot bring the
  database back, so what used to end in a fleet-wide `CrashLoopBackoff` now just marks
  the pods unready while they keep serving task history and the unreachable banner.
  Argo CD reachability stays absent from both probes — `GET /api/v1/reachability` and
  the `argocd_unavailable` metric report it instead.
- Graceful shutdown now fails readiness and keeps serving for five seconds before
  closing its listener, so an orchestrator can stop routing requests to the pod while
  it is still able to answer them. Rolling updates previously ended with a tail of
  connection resets, because endpoint removal is asynchronous and the listener closed
  the instant `SIGTERM` arrived. The wait is charged to the existing 25-second shutdown
  budget, so the whole sequence still fits the default 30-second grace period.
- The client now presents its credential — `ARGO_WATCHER_DEPLOY_TOKEN` or
  `BEARER_TOKEN` — on the status polls as well as on the task submission, so it keeps
  working against a server that requires one on reads. A server that does not require
  one ignores the header, and a client with no token configured is unchanged. The
  deploy token is dropped from a redirect that leaves the configured host, matching how
  Go already treats the `Authorization` header the JWT rides on.
- Read authorization is no longer sent to the OIDC provider on every request: a
  decision is reused for `OIDC_TOKEN_VALIDATION_INTERVAL`, capped by the token's own
  expiry, so an open browser tab costs one userinfo call per token per interval
  instead of one per refresh. Privileged actions (managing the deployment lock) still
  re-verify against the provider every time, so removing a user from a privileged
  group takes effect immediately.

  `OIDC_TOKEN_VALIDATION_INTERVAL` is now honoured — previous releases parsed it and
  never used it — and its default is `300000` ms (5 minutes). A deployment that set
  the variable explicitly will see its value take effect for the first time; one
  relying on the default gets 5-minute reuse for reads.
- A request whose credential could not be checked because the OIDC provider was
  unreachable now returns `503 Service Unavailable` instead of `401 Unauthorized`.
  The Web UI treats a 401 as a dead session and signs the user out, so a brief
  provider outage no longer logs everyone out.

### Fixed

- The Web UI no longer leaks a WebSocket connection when a page remount closes and
  immediately reopens its deployment-lock or Argo CD reachability subscription. A socket
  reports closed asynchronously, so the closing one used to report back after its
  replacement had already been opened, which discarded the replacement — leaving it
  connected with nothing tracking it — and opened a third connection. Each remount could
  do it again.
- With OIDC enabled, opening a task detail page from a shared link and pressing Back no
  longer bounces through a fresh sign-in that lands back on the same page; it returns to
  the task list. The provider's authorize URL is also no longer left in browser history,
  so the browser's own Back button behaves the same way.
- The Duration column of the tasks table now counts up while a deployment is in
  progress, instead of showing `0s` until the task reaches a final status.
- A deployment that fails with `Rollout status is not synced` now names what is
  actually out of sync. The report previously showed only the last sync *operation*,
  which routinely reads `Succeeded` / `successfully synced (all tasks run)` even
  though the application is still out of sync — leaving no culprit and reading as a
  contradiction. It now lists the drifted resources under `Out-of-sync resources:`
  and, when Argo CD could not complete the comparison at all, the reason under
  `Sync errors:`. An application that is degraded *and* out of sync is reported here
  too — a pending sync may still recover it — so the failing pod behind it now appears
  under `Unhealthy resources:` rather than only in the Argo CD UI. A failure with none
  of the three keeps its previous output.

### Security

- An application name from a task submitted without a credential is no longer used as a
  Prometheus label: `POST /api/v1/tasks` is open and the name is free text, so a caller
  could create a permanent series per request and exhaust the monitoring backend. Such
  deployments are now reported under `app="unknown"` in `processed_deployments`,
  `unauthenticated_reads`, and in `failed_deployment` when the failure precedes Argo CD
  confirming the application exists. Deployments carrying a deploy token, JWT or OIDC
  session keep their real application label; monitoring-only setups that submit without
  a credential lose the per-app breakdown in those three metrics.
- `GET /api/v1/config` no longer discloses how to reach a notification receiver: the
  `webhook` and `mattermost` blocks are reduced to their `enabled` flag. That endpoint
  cannot be authenticated — the Web UI reads the OIDC issuer and client id from it
  before it can hold a token — and a webhook URL is itself a credential. Every other
  field, including the `enabled` flags, is unchanged.
- Bumped `github.com/go-git/go-git/v5` to 5.19.2 for CVE-2026-71556, where worktree
  operations may follow symlinks outside the repository.

## [0.14.0] - 2026-08-04

### Added

- A deployment that requests an image the Argo CD application does not define now
  fails immediately instead of staying active until the task timeout expires. The
  usual cause is a misspelled image name or a registry prefix that does not match the
  manifests; the failure reason lists the images the application does define, so the
  correct value is visible without opening the Argo CD UI.

  The check compares the request against the application's *desired state* (Argo CD's
  managed resources), not its running pods, and runs once the application is synced
  and healthy but the expected image is still missing. Using the desired state is what
  makes it safe: `status.summary.images` reports only the images of live pods, so a
  workload that has no pod yet — an untriggered `CronJob`, a `Deployment` scaled to
  zero — still counts as defining its image and keeps waiting as before. Anything the
  check cannot resolve keeps waiting too, including a failed lookup or an application
  whose manifests declare no images at all.

  It is skipped entirely when `ARGO_REFRESH_APP` is `false` or a task sets
  `refresh: false`, because without a refresh the desired state may be older than the
  deployment.

  Two kinds of image are part of an application but never appear in the desired state
  Argo CD reports: images used only by a sync hook (Argo CD omits hook resources), and
  images named by a custom resource whose workload an operator creates out-of-band.
  Deploying those would hit the new failure despite being correct, so applications that
  do should carry the new annotation `argo-watcher/skip-image-validation: "true"`,
  which turns the check off for that application and restores the previous
  wait-for-the-timeout behaviour.

### Changed

- The client no longer tells you to check the logs for every failed deployment. The
  message now defers to the server's reason, which may point at the application or at
  the deployment request itself.
- A deployment that carries no `timeout` no longer logs that the task timeout is
  "non-positive", which read as a rejected value even though omitting the field is the
  normal case for clients that leave `TASK_TIMEOUT` unset. Debug logs now say the
  instance default is in use. A genuinely negative `timeout` is a malformed request and
  is logged as a warning, naming the value that was ignored.

### Fixed

- The status pills above the recent-tasks list now report the same numbers on every
  tab and reload together with the list. Selecting "In progress" or "Failed" used to
  drop the "All" count to the number of tasks matching the selected status — 0 on a
  tab whose status had none — and the per-status counts were fetched once and never
  refreshed, so a pill could keep claiming a task was in progress after it finished.
  While the counts are still loading, the pills show `—` rather than `0`.
- An image tag in the recent-tasks list no longer spills out of its badge. A tag
  containing a hyphen, such as `895-public`, could break after the hyphen and draw its
  second half below the badge; tags now stay on one line. A tag too long to fit is
  trimmed with an ellipsis instead of squeezing the image name out of the column, and
  the untrimmed value is available as a tooltip.
- On the task details page, a project that is a URL now starts on its own line below
  the `PROJECT` label instead of running on from it, and a long URL wraps rather than
  overflowing the card.
- Opening the Web UI now shows a loading screen — the logo and an animated indicator —
  while the session is resolved with the identity provider, instead of a blank white
  page that gave no sign the app was working. The status line starts as "Loading…" and
  becomes "Signing in…" only once the server configuration confirms OIDC is enabled, so
  an auth-less deployment is never told it is signing in; those deployments wait for a
  single configuration request and then render. The screen is always dark, regardless of
  the selected theme. A cold load still shows a brief blank page while the browser
  downloads the JavaScript bundle, before any of this can run.
- A deployment that fails because the application went `Degraded` after the new images
  landed is no longer reported as `ArgoCD API Error: application has degraded`. Argo CD's
  API had answered correctly — the application itself became unhealthy — and the message
  carried nothing else, so the cause had to be looked up in the Argo CD UI. Such a task
  now carries the same report as every other failed rollout: the application's sync and
  health status, any failed sync hooks, and the unhealthy resources enriched from Argo
  CD's live resource tree, which is where the actual culprit appears — the failed
  migration `Job` or the crashlooping pod behind the degradation.
- A failure reason no longer ends in an empty `Resources:` heading. Argo CD reports no
  health for resource kinds it cannot assess, and an application made up only of those
  produced a heading with nothing under it, which read as though the diagnostics had
  failed to collect. The heading is now printed only when there is something to list.
- A deployment that runs out its timeout while the application is still `Progressing`
  now names the resources that never became ready, under `Resources still progressing:`.
  Such a rollout has nothing degraded — every resource is mid-rollout — so the previous
  report identified no resource at all and gave no clue which workload was stuck. When
  something *is* degraded the report is unchanged: the degraded resource is the culprit,
  and its still-progressing siblings are left out rather than diluting it.

## [0.13.0] - 2026-07-30

### Added

- Optional batch write-back mode for the GitOps updater (`GIT_BATCH_WRITEBACK`,
  off by default). When enabled, concurrent write-backs to the same repository are
  coalesced into a single clone and a single push — one commit per application —
  instead of each taking the per-repository lock in turn, cutting the tail latency
  seen when many applications deploy to one GitOps repo at once. Batching is
  contention-driven, so it adds no latency when a repository is idle.
  `GIT_BATCH_MAX_SIZE` (default `20`) bounds how many applications are committed
  per flush, and the new `gitops_batch_size` metric reports how many were coalesced.
  On a graceful shutdown, queued write-backs stop retrying at the next retry
  boundary instead of spending the remaining budget, so their commits get a chance
  to land; keep `GIT_OP_TIMEOUT` under the shutdown budget so an attempt already in
  progress can finish too. This protects the commit, not the task's final status —
  a task interrupted by a restart can still be left `in progress` for the
  obsolete-task sweep to abort.
- `state_unavailable` metric: `1` when argo-watcher cannot reach its state backend
  (database), `0` otherwise. It tracks the state backend independently of ArgoCD.
- The manual deploy lock is now shared across replicas when `STATE_TYPE=postgres`.
  It is stored in a new `deploy_lock` table (migration `000006`), so a lock set
  through any replica rejects deployments on all of them and survives a restart —
  previously it lived in the process that served the request. With
  `STATE_TYPE=in-memory` the lock stays process-local, which remains correct for
  the single replica that backend supports.
- Support for any OpenID Connect provider (e.g. Authentik) for Web UI login and
  privileged-group authorization, not just Keycloak. Configure it with
  `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, and `OIDC_PRIVILEGED_GROUPS`; the backend
  discovers the userinfo endpoint automatically from the issuer's
  `.well-known/openid-configuration`. See the new OIDC / SSO Integration guide.

### Changed

- Graceful shutdown now completes within a single 25-second budget shared by all of
  its phases (draining HTTP requests, then WebSocket connections, then queued git
  write-backs), so it fits inside the Kubernetes default
  `terminationGracePeriodSeconds` of 30. Each phase previously carried its own
  independent timeout, letting the sequence run long enough for the kubelet to
  `SIGKILL` the process partway through — which cut off exactly the work the
  shutdown sequence exists to finish. Lower the grace period below 30 seconds and
  the drain can still be cut short; see the troubleshooting guide.
- Releasing the deploy lock during a scheduled lockdown window now records a
  15-minute override deadline in shared state instead of an in-process timer, so
  the suppression ends at the same instant on every replica and survives a
  restart. Setting the lock again clears a pending suppression.
- If the deploy lock state cannot be read (database unreachable, or migrations not
  applied), the server now rejects deployments as if a lock were active rather
  than letting them through. The Web UI lockdown banner also reacts within a few
  seconds instead of up to a minute.
- The `POST` and `DELETE /api/v1/deploy-lock` endpoints now return `500` when the
  lock state cannot be persisted, instead of reporting success.
- The Web UI "unreachable" banner now names exactly which dependency is down —
  ArgoCD, the state backend (database), or both — instead of always hedging with
  "ArgoCD or its state backend". It is also anchored to the bottom of the page for
  consistency with every other error; the deploy-lock banner yields to it when
  both conditions apply.
- The `argocd_unavailable` metric is now raised **only** for ArgoCD outages. A
  state-backend (database) outage is reported by the new `state_unavailable`
  metric instead — previously either outage raised `argocd_unavailable`.
- **Breaking (frontend / polling API):** the read-only reachability endpoint moved
  from `GET /api/v1/argocd-status` to `GET /api/v1/reachability` and now returns
  `{"available":bool,"reason":"argocd"|"database"|"both"}` (`reason` omitted when
  available) instead of a bare boolean. The WebSocket outage broadcast now carries
  the cause as `argocd_down:<reason>`; recovery is still `argocd_up`.
- `LOG_LEVEL=debug` no longer logs a line for every request to the `/ws` WebSocket
  endpoint, which previously drowned out the rest of the debug output. Requests
  that fail to upgrade are still logged, together with the `Upgrade` and
  `Connection` headers that actually arrived, so a reverse proxy stripping them
  stays diagnosable — see the new troubleshooting entry for a Web UI that does not
  update without a refresh.
- `GET /api/v1/config` now exposes the authentication settings under an `oidc` key.
  The legacy `keycloak` key is still emitted with identical content for backward
  compatibility. Privileged requests (rollback, deploy-lock) may use the
  `Oidc-Authorization` header; the legacy `Keycloak-Authorization` header is still
  accepted.

### Deprecated

- The `KEYCLOAK_*` environment variables are deprecated in favour of their `OIDC_*`
  equivalents (`OIDC_ENABLED`, `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`,
  `OIDC_TOKEN_VALIDATION_INTERVAL`, `OIDC_PRIVILEGED_GROUPS`). They remain fully
  honored, so existing Keycloak deployments need no changes — the issuer is
  synthesized from `KEYCLOAK_URL` + `KEYCLOAK_REALM`.

### Fixed

- Argo CD connectivity failures during a deployment check — request timeouts, DNS
  and TLS errors, and `5xx` responses from Argo CD or a proxy in front of it — are
  now consistently reported with the `aborted` status and a descriptive reason,
  matching the existing behaviour for a refused TCP connection. Previously only a
  refused connection was recognised and the other outages surfaced as a generic
  `failed` status that blamed the application. Aborted deployments continue to
  count toward `failed_deployment` (a deployment that could not be confirmed is
  still a failure); the `argocd_unavailable` metric indicates when Argo CD itself
  was the cause.
- The CLI client now reports the `aborted` status with a clear message and the
  task's status reason, instead of the misleading "unexpected deployment status …
  the client may be out of date". The non-zero exit code is unchanged.
- Stale in-progress tasks aborted by the obsolete-task sweep now record a status
  reason ("did not complete within the staleness window"), distinguishing them
  from deployments aborted because Argo CD was unreachable.
- The Web UI now re-reads the deploy lock state over REST every time its WebSocket
  (re)connects, matching what the "unreachable" banner already did. The server only
  pushes lock changes as transitions, so a lock set while a browser's socket was
  down — or before its initial read failed — previously stayed invisible until a
  manual page refresh, hiding an active deployment freeze. This matters more now
  that the lock is shared, because the transition can come from another replica.
- The lockdown watcher is now the only thing that broadcasts deploy-lock changes to
  Web UI clients; the `POST`/`DELETE /api/v1/deploy-lock` handlers no longer push
  directly. Two independent notifiers could leave the watcher's idea of the last
  broadcast state stale, so a lock re-acquired through another replica within one
  poll interval was never announced — leaving the serving replica's clients showing
  unlocked while deployments were in fact frozen. Clients on the replica that serves
  a lock change now see the banner within one poll interval (a few seconds) instead
  of instantly, which is the delay every other replica already had; the operator who
  made the change still sees their own result immediately.
- The Web UI no longer replays a stale banner state after every listener has
  detached and a new one subscribes. A reachability or deploy-lock request already
  in flight at that moment repopulated the cache the detach had just cleared, so the
  next subscriber was handed a value read before the gap instead of a fresh one —
  which could show ArgoCD as reachable during a real outage. In production the
  providers mount once, but React's development double-mount hit this on every
  reload.

### Security

- A deployment can no longer cancel one that was submitted with more authority than
  itself. Task submission without a credential is accepted by design (the rollout is
  tracked, but not written back to git), but superseding let such a task cancel any
  in-flight deployment for the same application and image name — and a cancelled
  task aborts its pending git write-back, so a credentialed deployment's image tag
  never landed. Superseding now also requires that the in-flight task presented no
  more authority than the new one, which needs no configuration and leaves
  token-less setups behaving exactly as before. Whether a task was credentialed is
  persisted (migration `000007`); tasks already in flight during the upgrade read as
  uncredentialed until they finish. Task creation fails with `503` until that
  migration has been applied, so it must run before or alongside the new server image
  — which is what the Helm chart's pre-upgrade migration Job already guarantees. The
  flag is now neither accepted from nor emitted in API payloads — with the in-memory
  state backend, task listings previously echoed a `validated` key.
- Update frontend dependencies to clear the actionable Dependabot advisories: bump
  `postcss` to 8.5.23 (path traversal via the `sourceMappingURL` previous-source-map
  auto-loader; build-time only) and the `dompurify` pin to 3.4.12 (elements allowed
  through `CUSTOM_ELEMENT_HANDLING` bypassed the `afterSanitizeElements` hook).
  `react-router` deliberately stays on 7.18.1: the advisory affects only the
  unstable RSC APIs, which the Web UI does not use, and the fix ships in 8.3.0 —
  outside the range React Admin supports.

## [0.12.2] - 2026-07-21

### Added

- The Web UI now shows a prominent "ArgoCD unreachable" banner whenever Argo
  Watcher cannot reach Argo CD (or its state backend), so operators can tell an
  outage apart from "no recent deployments" without reading server logs or
  scraping Prometheus. The banner appears and clears live via the existing
  WebSocket. A read-only `GET /api/v1/argocd-status` endpoint exposes the same
  cached reachability as a plain boolean for external polling.

### Changed

- Submitting a deployment (`POST /api/v1/tasks`) while Argo CD is unreachable now
  fails fast with `503 {"status":"down"}` using the cached reachability state,
  instead of blocking on the full Argo CD API retry budget until the client's own
  HTTP timeout fired and masked the cause as an opaque `context deadline
  exceeded`.
- The server and `--migrate` now bound the initial PostgreSQL connection with a
  `connect_timeout` (new `DB_CONNECT_TIMEOUT` env var, default `10` seconds) and
  log a `Connecting to PostgreSQL database...` line before dialing, so an
  unreachable database fails fast with a diagnostic signal instead of blocking on
  the OS TCP timeout with no logs. The timeout is enforced even when `DB_DSN` is
  supplied explicitly. Deployments with high-latency links to Postgres may need to
  raise `DB_CONNECT_TIMEOUT`; a non-positive value is rejected at config-load time
  on both paths.
- The `--migrate` command now emits structured JSON logs honoring `LOG_LEVEL`,
  consistent with the server, instead of the previous plain-text output.

## [0.12.1] - 2026-07-20

### Changed

- Required configuration variables are now rejected when set to an empty
  string, not only when unset — previously the server and client accepted an
  empty required value and only failed later (e.g. at connect or request time).
  `go-playground/validator` was dropped in favour of the env loader's native
  `required,notEmpty` checks; the server config's allowed-value and numeric-range
  rules are now explicit checks that keep the same grouped, one-pass error output.
- Server and deployment-watcher logs now use structured `slog` key/value
  attributes (e.g. `error`, `app`, `id`, `status`) instead of interpolating
  values into the message string, and some message texts were tightened. Log
  consumers that match on the old message strings may need updating. One
  deployment-failure event that was logged at info is now logged at warning,
  matching the sibling failure path. The client CLI and migration tool keep
  their plain-text log output.

### Added

- New `deployment_duration_seconds` Prometheus histogram (label `app`) recording
  the end-to-end wall-clock time of a successful deployment, from the start of
  rollout monitoring until the app reached the deployed state. Surfaced as a
  per-application percentile panel on the example Grafana dashboard.
- Example Grafana dashboard (`monitoring/grafana/dashboards/argo-watcher.json`)
  visualizing every exposed Prometheus metric, with a per-application drill-down
  driven by an `Application` variable. A `monitoring` docker-compose profile runs
  Prometheus and Grafana with the datasource and dashboard pre-provisioned
  (`docker compose --profile monitoring up`).

## [0.12.0] - 2026-07-15

### Added

- New `gitops_writeback_duration_seconds` and `gitops_lock_wait_duration_seconds`
  Prometheus histograms (label `app`) to surface slow or contended git write-backs
  to GitOps repositories — the first measures how long a write-back holds the
  per-repository lock (clone/commit/push plus retries), the second how long a
  deployment waits to acquire it.

### Changed

- Enrich deployment-failure reasons with the actual root cause from ArgoCD's live
  resource tree. When a rollout fails — both "not available" and "not healthy" —
  the task status reason now surfaces the failing pod's condition (e.g. an
  `ImagePullBackOff`/`ErrImagePull` or crash-loop message), which the previous
  top-level resource summary never carried, alongside the existing failed-hook and
  terminal sync-operation diagnostics. No new stored data — the existing
  `status_reason` is simply more actionable, so no database migration is required.
- Speed up GitOps write-backs by detecting whether the override file changed with a
  targeted single-file comparison instead of scanning the entire working tree. On
  large GitOps repositories this markedly reduces per-deployment commit latency,
  most noticeably when several deployments to the same repository run concurrently
  and each must wait its turn under the per-repository lock.
- Clone and fetch GitOps repositories shallowly (depth 1, no tags) instead of pulling
  full history. On a repository with a deep history (100k+ commits) a full clone could
  take minutes while holding the per-repository write-back lock, blocking every other
  deployment to that repository; the shallow clone caps this to seconds. Commit, push,
  retry/self-heal, and the persistent on-disk cache behave exactly as before.

### Fixed

- Return `500 Internal Server Error` instead of `404 Not Found` when looking up a
  task by id fails for a backend reason (e.g. the database is unreachable).
  Previously every error from the task lookup was reported as `404`, so a database
  outage masqueraded as a missing task — hiding the failure from metrics and
  alerting and leaking the raw backend error to the client. Genuine "no such task"
  (including a malformed task id) still returns `404`; the `500` response body no
  longer exposes internal error detail.
- Wait for in-flight WebSocket upgrades during graceful shutdown. The shutdown
  routine only tracked established connections, not handshakes still being
  negotiated, which left a data race between an in-progress upgrade and shutdown
  (surfaced by the race detector). Handshakes are now accounted for, so shutdown
  drains them cleanly. Shutdown also stops accepting new connections before
  draining the WebSocket goroutines, avoiding a rare shutdown-time panic when a
  new connection arrived mid-drain.
- Stop sending a stale "locked" WebSocket notification after a manual lock
  release. Releasing a manual lock during an active scheduled window suppresses
  that window for 15 minutes; when the timer expired the server always told
  clients the system was "locked" again, even if the scheduled window had ended
  in the meantime — leaving the Web UI showing a lockdown that was no longer in
  effect. It now re-notifies only when the system is genuinely still locked.
- Never show React-admin's built-in username/password login form. The Web UI
  authenticates only through a top-level Keycloak redirect, but the stock login
  form could still surface as a misleading fallback when Keycloak was
  misconfigured; it is now disabled outright.
- Keep the History page filters visible when there are no matching deployments.
  Previously, with no deployments in the default time window the empty-state
  message replaced the whole view, hiding the date-range and application
  filters so users had no way to widen the range.
- Report the correct final status in failure notifications. When a deployment
  failed (Argo CD unreachable, application not found), the stored task status
  was correct but the outgoing result notification still carried "in progress",
  so webhook consumers never saw the failure and Mattermost posted it as a new
  "started" message instead of a threaded result.
- Fail a deployment when a watcher-managed image is missing its image-tag
  annotation, instead of silently skipping the git write-back and reporting
  success. Previously an application whose `argo-watcher/managed-images` listed
  an image without a matching `*.helm.image-tag` annotation logged an error,
  wrote nothing to git, and still marked the deployment successful.
- Keep the task list responsive and populated when Argo CD is unreachable.
  Listing tasks performed a live Argo CD login check on every read, so a DNS or
  network outage made the list hang for the full API retry budget and then
  render empty — hiding task history that was sitting untouched in the state
  store. Listing now reads straight from the state backend; the Argo CD check is
  retained only on the deployment-creation path, which genuinely requires it.
- Show an explicit, retryable error in the Web UI when the task list fails to
  load, instead of leaving it stuck in the loading skeleton or rendering a
  misleading "no tasks" placeholder. Web UI requests are now bounded by a
  30-second client-side timeout, so a hung backend can no longer pin the table
  in its skeleton state indefinitely.

### Security

- Only expose the manual deploy-lock endpoints (`POST`/`DELETE
  /api/v1/deploy-lock`) when Keycloak is enabled. Without an authentication
  backend these state-changing endpoints were reachable unauthenticated,
  letting anyone able to reach the server freeze or release all deployments;
  they are now registered only when Keycloak is enabled, and the Web UI hides
  the manual lock toggle to match. The read-only lock status and scheduled
  lockdown are unaffected.
- Redact the authorization credential from the client's debug output. With debug
  mode enabled the client logged an equivalent cURL command that included the
  `Authorization` (JWT) and `ARGO_WATCHER_DEPLOY_TOKEN` header values in clear
  text, leaking the deploy credential into CI job logs and log aggregators. Those
  header values are now replaced with `<redacted>` while the header names remain
  visible for troubleshooting.

## [0.11.0] - 2026-07-13

### Added

- Detect and flag rollback deployments: when a task redeploys an image set that
  ran earlier for the same application (returning to a previous version), it is
  marked as a rollback. The task tables show a marker next to the status, and
  the task detail page links to the earlier task the deployment rolls back to.
- Expose `IsRollback` and `RollbackTargetId` as webhook notification template
  variables so alerts can highlight rollbacks.
- Cancel superseded deployments: when a new deployment is triggered while a
  previous in-progress one for the same application targets one of the same
  images, the older deployment is cancelled and marked with the new `cancelled`
  status instead of continuing to poll Argo CD until it times out. Matching on
  image name (not just the application) lets independent per-image deployments of
  the same application run concurrently without cancelling each other. The CLI
  client reports the cancellation and the status is filterable in the Web UI
  (#353).
- Per-task Argo CD refresh override: set `TASK_REFRESH=true`/`false` on the CLI
  client (or `refresh` in the task JSON) to override the server's instance-wide
  `ARGO_REFRESH_APP` default for a single deployment. Setting it to `false` for
  applications that never settle a refresh (e.g. one with a constantly
  reconciling CronJob) avoids the status check timing out (#334).
- New `argocd_refresh_duration_seconds` Prometheus histogram (label `app`) to
  surface slow or stuck Argo CD refreshes.
- Mattermost notification strategy (`MATTERMOST_ENABLED`, `MATTERMOST_URL`,
  `MATTERMOST_TOKEN`, `MATTERMOST_CHANNEL_ID`, `MATTERMOST_FORMAT`,
  `MATTERMOST_MENTION_AUTHOR`) alongside the generic webhook. Instead of one
  independent message per event, it posts the deployment start as a root channel
  post and the result as a thread reply, optionally prefixing `@<Author>` to
  notify the deploy author. Requires a Mattermost bot account with access to the
  target channel. The start-to-thread mapping is kept in memory, so a restart
  mid-deployment or a multi-replica setup degrades gracefully to a regular
  channel post for the result (#460).

### Changed

- The CLI client now treats any unrecognized deployment status as terminal and
  exits with an error instead of polling in a tight loop. **Upgrade CLI clients
  to this version**: older clients do not understand the new `cancelled` status
  and will busy-loop against the server if one of their deployments is superseded
  (#353).
- Group and humanize server startup misconfiguration errors: missing required
  and invalid environment variables are now reported together in a single
  message listing every offending variable, so you can fix them all in one pass
  instead of one restart at a time.
- The CLI client now surfaces the server's response body on HTTP failures and,
  on `401`/`403`, hints which token variables govern authentication
  (`ARGO_WATCHER_DEPLOY_TOKEN` / `BEARER_TOKEN`), replacing the previous
  status-code-only message.
- `BEARER_TOKEN` can now be set to the raw JWT with no `Bearer ` prefix, so the
  value is maskable as a GitLab CI variable (the space in `Bearer ` blocked
  masking). The `Bearer <jwt>` form is still accepted for backward
  compatibility — the client strips the prefix before sending.
- Update backend and frontend dependencies to their latest releases. The bundled
  web UI now runs on React 19 and Material UI 9, and building from source
  requires Go 1.26.
- Notify the Web UI when a scheduled lockdown window automatically begins or
  ends. Previously only manual lock/unlock pushed live updates, so a UI opened
  before a scheduled window started never showed the lockdown banner without a
  page refresh; connected clients are now notified within about a minute (#302).
- Release images now publish a single multi-arch manifest tag
  (`ghcr.io/shini4i/argo-watcher:<tag>` and the `-client` image) instead of
  separate per-architecture `-amd64`/`-arm64` tags; pull the plain tag going
  forward. Each published image now also ships an attached SBOM.
- Harden the GitOps write-back against concurrent writers on a shared repo: the
  retry now uses a jittered capped-exponential backoff (fast early retries win a
  push race) instead of a fixed 2s delay, and the default `GIT_MAX_ATTEMPTS` is
  raised from 3 to 5. A task superseded by a newer deployment for the same
  application now aborts its write-back (re-checked before every attempt) rather
  than committing a stale image tag, so the larger retry budget cannot let an
  older deployment overwrite a newer one.
- Switch server and mock logging from zerolog to Go's standard library
  `log/slog`. Log output is still JSON on stderr, but level names are now
  uppercase (e.g. `INFO`), the message field key is `msg` (previously
  `message`), timestamps carry nanosecond precision, and durations are reported
  in nanoseconds — update any log processing that keys on the old field names or
  values. `LOG_LEVEL` still accepts `debug`/`info`/`warn`/`error` (default
  `info`); the previously-accepted, undocumented `disabled` value is no longer
  recognized and now falls back to `info`.

### Fixed

- Retry transient failures (network errors or `5xx` responses) up to 3 times
  with a 2-second backoff while the CLI client polls the server for deployment
  status, instead of aborting the pipeline on the first blip. Terminal failures
  (`4xx`, invalid tokens, malformed responses) still fail fast, and task
  submission is not retried (#217).
- Enforce the deployment timeout (`DEPLOYMENT_TIMEOUT` / per-task timeout) as a
  real wall-clock deadline instead of a fixed number of status-check attempts.
  When the Argo CD API responded slowly, a rollout could previously run well
  past its configured timeout; the deadline now also cancels in-flight Argo CD
  API calls, so a deployment can no longer overrun the configured duration
  (#304).
- Reject invalid or unauthorized tokens with `401 Unauthorized` and an
  actionable reason instead of `500 Internal Server Error`, and distinguish a
  missing token from an invalid one in the `401` response.

## [0.10.7] - 2026-06-30

### Fixed

- Fix a Keycloak redirect loop that appeared *after* a successful login, where
  the browser bounced between the app and the Keycloak login page without ever
  settling. The login callback is now processed during app startup, before the
  router runs its initial redirect, so the authorization code is no longer
  discarded. Keycloak-less deployments are unaffected and continue to render
  immediately.

## [0.10.6] - 2026-06-30

### Added

- Publish `llms.txt` and `llms-full.txt` on the documentation site, following
  the [llmstxt.org](https://llmstxt.org/) standard, so AI agents can discover
  and consume the docs.

### Fixed

- Fix an infinite redirect loop on Keycloak-protected instances where users who
  already had a valid session were bounced between the app and the Keycloak
  login/logout pages, and were sometimes silently logged out. The login flow now
  authenticates through a top-level redirect (`login-required`) instead of a
  cross-site silent iframe, whose third-party cookies modern browsers strip.

### Security

- Update dependencies to clear all open Dependabot advisories. Backend: bump
  `go-git` to 5.19.1 (malformed-object DoS, crafted-repo `.git` write, SSH
  argument escaping) and `quic-go` to 0.60.0 (HTTP/3 QPACK memory exhaustion).
  Frontend: bump `react-router` to 6.30.4 (protocol-relative open redirect),
  pin `dompurify` to 3.4.11 (sanitization-bypass advisories), and bump `vite`
  to 7.3.6 along with the transitive `esbuild`, `form-data`, `@babel/core`,
  `js-yaml`, and `ws` packages.

## [0.10.5] - 2026-06-12

### Added

- `GIT_OP_TIMEOUT` (default `90s`): per-attempt wall-clock budget for one
  clone + update cycle in the GitOps updater.
- `GIT_MAX_ATTEMPTS` (default `3`): total git update attempts (initial +
  retries) before giving up. The final attempt invalidates the on-disk cache
  and performs a fresh clone, so a poisoned cache self-heals without operator
  intervention.
- Dedicated `Security` CI workflow running gosec, govulncheck, Trivy (backend +
  frontend dependency vulnerabilities), and TruffleHog (secret scan).
- Nuclei DAST job that builds the shipped artifact, runs it, and scans the live
  HTTP surface (API and served frontend) with a passive template sweep and
  active OpenAPI-seeded fuzzing; fails the build on medium-or-higher findings
  and uploads results to the code-scanning dashboard.
- `Workflow Audit` CI workflow running zizmor against the GitHub Actions
  definitions.
- Local TruffleHog pre-commit hook so secrets never reach a commit.
- Keycloak-based end-to-end auth tests: a real Keycloak (docker-compose
  `integration` profile, imported from a test realm) verifies that only
  privileged-group users can set or release the deploy lock.
- A Keycloak-enabled nuclei DAST pass that fuzzes the authenticated API surface
  with a privileged token.

### Changed

- **Breaking:** Replaced the GitOps updater's single total wall-clock timeout
  with a retry-based model. A git update is now bounded per attempt
  (`GIT_OP_TIMEOUT`) and retried up to `GIT_MAX_ATTEMPTS` times, instead of
  sharing one total budget across clone + commit + push + race recovery. The
  worst-case wall clock is `GIT_OP_TIMEOUT × GIT_MAX_ATTEMPTS` plus inter-attempt
  backoff.
- Hardened all GitHub Actions workflows: job-scoped least-privilege
  permissions, `persist-credentials: false` on checkouts, and every third-party
  action pinned to a commit SHA.
- Renamed the test workflow from `run-tests-and-sonar-scan.yml` to
  `run-tests.yml` (it no longer references Sonar).
- Added `govulncheck`, `trivy`, `trufflehog`, and `zizmor` to the Nix devshell.

### Deprecated

- `GIT_TIMEOUT`: superseded by `GIT_OP_TIMEOUT`. When `GIT_TIMEOUT` is set and
  `GIT_OP_TIMEOUT` is not, the legacy value is used directly as `GIT_OP_TIMEOUT`
  (1:1 mapping, preserving the original per-call budget) and a deprecation
  warning is logged. Set `GIT_OP_TIMEOUT` explicitly to silence it.

### Removed

- **Breaking:** `EXTRA_PUSH_RACE_MARKERS` and the substring-based push-race
  error detection it extended. Push-race recovery is now handled by the retry
  loop and cache self-heal rather than error-message matching.

### Security

- Bumped `golang.org/x/crypto` to `v0.53.0`, resolving 7 vulnerabilities
  (GO-2026-5013, GO-2026-5015 and others) reachable through the SSH push path
  and surfaced by the new govulncheck gate.
- Bumped the Go toolchain to `1.25.11`, resolving a `net/textproto` standard
  library vulnerability present in `go1.25.9`.

[Unreleased]: https://github.com/shini4i/argo-watcher/compare/v0.15.0...HEAD
[0.15.0]: https://github.com/shini4i/argo-watcher/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/shini4i/argo-watcher/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/shini4i/argo-watcher/compare/v0.12.2...v0.13.0
[0.12.2]: https://github.com/shini4i/argo-watcher/compare/v0.12.1...v0.12.2
[0.12.1]: https://github.com/shini4i/argo-watcher/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/shini4i/argo-watcher/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/shini4i/argo-watcher/compare/v0.10.7...v0.11.0
[0.10.7]: https://github.com/shini4i/argo-watcher/compare/v0.10.6...v0.10.7
[0.10.6]: https://github.com/shini4i/argo-watcher/compare/v0.10.5...v0.10.6
[0.10.5]: https://github.com/shini4i/argo-watcher/compare/v0.10.4...v0.10.5
