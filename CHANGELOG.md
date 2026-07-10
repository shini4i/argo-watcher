# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Detect and flag rollback deployments: when a task redeploys an image set that
  ran earlier for the same application (returning to a previous version), it is
  marked as a rollback. The task tables show a marker next to the status, and
  the task detail page links to the earlier task the deployment rolls back to.
- Expose `IsRollback` and `RollbackTargetId` as webhook notification template
  variables so alerts can highlight rollbacks.
- Cancel superseded deployments: when a new deployment for an application is
  triggered while a previous one is still in progress, the older deployment is
  cancelled and marked with the new `cancelled` status instead of continuing to
  poll Argo CD until it times out. The CLI client reports the cancellation and
  the status is filterable in the Web UI (#353).

### Changed

- Group and humanize server startup misconfiguration errors: missing required
  and invalid environment variables are now reported together in a single
  message listing every offending variable, so you can fix them all in one pass
  instead of one restart at a time.
- The CLI client now surfaces the server's response body on HTTP failures and,
  on `401`/`403`, hints which token variables govern authentication
  (`ARGO_WATCHER_DEPLOY_TOKEN` / `BEARER_TOKEN`), replacing the previous
  status-code-only message.
- Update backend and frontend dependencies to their latest releases. The bundled
  web UI now runs on React 19 and Material UI 9, and building from source
  requires Go 1.26.

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

[Unreleased]: https://github.com/shini4i/argo-watcher/compare/v0.10.7...HEAD
[0.10.7]: https://github.com/shini4i/argo-watcher/compare/v0.10.6...v0.10.7
[0.10.6]: https://github.com/shini4i/argo-watcher/compare/v0.10.5...v0.10.6
[0.10.5]: https://github.com/shini4i/argo-watcher/compare/v0.10.4...v0.10.5
