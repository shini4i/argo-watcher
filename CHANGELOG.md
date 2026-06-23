# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/shini4i/argo-watcher/compare/v0.10.5...HEAD
[0.10.5]: https://github.com/shini4i/argo-watcher/compare/v0.10.4...v0.10.5
