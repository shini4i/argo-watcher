# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Dedicated `Security` CI workflow running gosec, govulncheck, Trivy (backend +
  frontend dependency vulnerabilities), and TruffleHog (secret scan).
- Nuclei DAST job that builds the shipped artifact, runs it, and scans the live
  HTTP surface (API and served frontend) with a passive template sweep and
  active OpenAPI-seeded fuzzing; fails the build on medium-or-higher findings
  and uploads results to the code-scanning dashboard.
- `Workflow Audit` CI workflow running zizmor against the GitHub Actions
  definitions.
- Local TruffleHog pre-commit hook so secrets never reach a commit.

### Changed

- Hardened all GitHub Actions workflows: job-scoped least-privilege
  permissions, `persist-credentials: false` on checkouts, and every third-party
  action pinned to a commit SHA.
- Renamed the test workflow from `run-tests-and-sonar-scan.yml` to
  `run-tests.yml` (it no longer references Sonar).
- Added `govulncheck`, `trivy`, `trufflehog`, and `zizmor` to the Nix devshell.

### Security

- Bumped `golang.org/x/crypto` to `v0.53.0`, resolving 7 vulnerabilities
  (GO-2026-5013, GO-2026-5015 and others) reachable through the SSH push path
  and surfaced by the new govulncheck gate.
- Bumped the Go toolchain to `1.25.11`, resolving a `net/textproto` standard
  library vulnerability present in `go1.25.9`.

[Unreleased]: https://github.com/shini4i/argo-watcher/compare/v0.10.4...HEAD
