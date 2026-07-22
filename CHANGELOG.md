# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/shini4i/argo-watcher/compare/v0.12.2...HEAD
[0.12.2]: https://github.com/shini4i/argo-watcher/compare/v0.12.1...v0.12.2
[0.12.1]: https://github.com/shini4i/argo-watcher/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/shini4i/argo-watcher/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/shini4i/argo-watcher/compare/v0.10.7...v0.11.0
[0.10.7]: https://github.com/shini4i/argo-watcher/compare/v0.10.6...v0.10.7
[0.10.6]: https://github.com/shini4i/argo-watcher/compare/v0.10.5...v0.10.6
[0.10.5]: https://github.com/shini4i/argo-watcher/compare/v0.10.4...v0.10.5
