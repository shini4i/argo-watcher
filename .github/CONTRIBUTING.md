# Contributing to Argo Watcher

Contributions are welcome. This document describes what is expected of a change before it can be merged, so that review time is spent on the substance of your work rather than on process.

For local environment setup, see the [Development guide](../docs/contributing/development.md). For documentation wording and formatting, see the [Style Guide](../docs/contributing/style.md).

## Open an Issue First

**Before you write code, open an issue describing the change you intend to make.** This is strongly encouraged for anything beyond a one-line fix, and it exists to save your time, not to gate your work.

A short discussion up front avoids the outcomes that waste the most effort:

- The change conflicts with a design decision that is not obvious from the code.
- The feature is already implemented behind a configuration flag, or is deliberately absent.
- The problem is real but the fix belongs in a different layer.
- Someone is already working on it.

A rejected PR after a weekend of work is a bad experience for everyone. A two-message issue thread prevents it.

**Security vulnerabilities are the exception: do not open an issue.** Report them privately as described in the [Security Policy](SECURITY.md).

## What Will Not Be Merged

Low-value drive-by pull requests are closed without detailed review:

- Single typo or wording fixes in comments, docs, or strings.
- Cosmetic reformatting, whitespace changes, or import reordering.
- Renaming identifiers or restructuring code without a behavioral reason.
- Dependency bumps — Dependabot already handles these.
- Refactors whose justification is stylistic preference.

This is not about the fix being wrong. Reviewing, discussing, and merging a PR costs more maintainer attention than the change is worth, and a repository this size cannot absorb that overhead.

**If you spot a typo or a rough edge, open an issue instead.** Reports are genuinely welcome and get fixed — it is the per-PR overhead that does not scale.

## AI-Assisted Contributions

AI-assisted contributions are fine. There is no disclosure requirement and no separate review track: **the result is evaluated, not the tooling that produced it.**

The bar is the same one that applies to hand-written code, which in practice means:

- You have read every line of your diff and can explain why it is written that way.
- You can respond to design questions in review without regenerating the patch.
- The tests pass locally, and the tests you added actually exercise the change.
- The change stays inside the scope of the issue it addresses.

Unreviewed generated output — code that does not build, tests that assert nothing, invented APIs, or a diff far larger than the problem — is treated as a low-value PR and closed. The reason is the absence of review, not the use of AI.

## Project Standards

### Go

- **Formatting.** `go fmt` and `goimports`, enforced by pre-commit hooks (`go-fmt`, `go-imports`, `go-mod-tidy`). Install them with `pre-commit install`.
- **Toolchain.** The Go version in `go.mod` is authoritative; do not lower it.
- **Package layout.** Application code lives under `internal/` (`argocd`, `auth`, `client`, `config`, `helpers`, `lock`, `logging`, `migrate`, `models`, `notifications`, `prometheus`, `server`, `state`, `updater`). Binaries are thin wrappers under `cmd/` (`argo-watcher`, `client`, `mock`).
- **Logging.** Standard-library `log/slog`. Do not introduce another logging library.
- **Configuration.** New settings are struct fields with `env:` tags in `internal/config`, not ad-hoc `os.Getenv` calls.
- **Doc comments.** Exported identifiers carry a doc comment, and non-obvious behavior is explained where it lives. When you change behavior, update the comment in the same commit — a stale comment is worse than none.

### Frontend

The UI in `web/` is React and TypeScript. Lint with `task lint-web` and test with `task test-web`.

### Database

Schema changes ship as a new numbered pair of migration files in `db/migrations/` — both `.up.sql` and `.down.sql`. Never edit a migration that has already been released.

### API Documentation

The Swagger spec is generated from [swag](https://github.com/swaggo/swag) annotations on the handlers, so those annotations are the source of truth: when you add a route or change a request or response model, update the annotations in the same commit.

The generated `web/public/swagger/swagger.json` is gitignored — there is nothing to commit. It is rebuilt automatically by `task test` and `task build`, and by the release pipeline.

### Commits and PR Titles

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description`, imperative mood, no trailing period. Types in use: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`, `perf`.

Pull requests are squash-merged and **the PR title becomes the commit subject on `main`**, so the title must follow the same convention.

### Changelog

User-facing changes get an entry under `## [Unreleased]` in [`CHANGELOG.md`](../CHANGELOG.md), following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Describe the change from the user's perspective — what is now possible, or what behavior changed — not which functions you touched. Internal refactors, test-only changes, and CI edits do not need an entry.

### Documentation

If your change alters observable behavior, configuration, or setup, update the affected page under `docs/` in the same PR.

### Secrets

A TruffleHog pre-commit hook scans staged files, and CI scans the pushed range. Never commit credentials, even for local development, without adding the file to both exclude lists with a justification.

## Test Coverage

**Every change ships with tests.** This is the requirement most likely to send a PR back.

- **New behavior** needs unit tests covering the intended path and the failure paths.
- **A bug fix** needs a test that fails before your fix and passes after it. Include it even when the fix is one line — it is what stops the bug from returning.
- Tests live in `_test.go` files beside the code they cover.
- The suite uses [testify](https://github.com/stretchr/testify) (`assert` for ordinary checks, `require` where a failure makes the rest of the test meaningless) and table-driven cases for anything with more than two variants.
- Interfaces are mocked with [gomock](https://github.com/uber-go/mock). Mocks are generated into `internal/mocks/`, which is gitignored — `task test` regenerates them, and changed interface files are picked up automatically. If you introduce a *new* mocked interface, add its `mockgen` line and its source path to the `mocks` task in `Taskfile.yml`.

Run the suites locally before opening a PR:

| Command | Scope |
|---------|-------|
| `task test` | Backend unit tests, with mocks and the Swagger spec generated first. Needs Postgres on `localhost:5432` — `docker compose up -d postgres migrations` provides it with the expected credentials |
| `task test-web` | Frontend unit tests |
| `task test-integration` | GitOps updater against real Gitea with Toxiproxy fault injection, plus the Keycloak auth flow (needs Docker) |

Coverage is reported to Codecov on every pull request. A patch that lowers coverage needs a reason stated in the PR description.

## Large Features: Add an E2E Phase

For anything substantial — a new deployment mode, a new integration, a metric, a configuration toggle that changes runtime behavior — **you are encouraged to cover it in the end-to-end lab** under [`test/e2e/`](../test/e2e/README.md), not only in unit tests.

The lab boots a disposable [kind](https://kind.sigs.k8s.io/) cluster running **real** Argo CD, Gitea, and argo-watcher built with the race detector, so it exercises what mocks cannot: the real Argo CD polling loop, the real git push path, and behavior under sustained concurrency. Each feature is covered by a named phase — see the lab's README for the phase list, the prerequisites, and how to add one.

The lab is not part of the standard PR checks because it is expensive. Maintainers run it by adding the `e2e` label to a pull request.

## Before You Open the Pull Request

- The change was discussed in an issue.
- `task test` and `task test-web` pass locally.
- `pre-commit run --all-files` is clean.
- New behavior has tests; a bug fix has a regression test.
- `CHANGELOG.md` has an `[Unreleased]` entry if the change is user-facing.
- Affected documentation under `docs/` is updated.
- The PR title is a valid Conventional Commit.

## What CI Checks

These must be green before a merge: backend tests, frontend tests, integration tests, the Keycloak auth end-to-end job, the Go scanners (gosec and govulncheck), Trivy, retire.js, TruffleHog, CodeQL, and SonarCloud.

The nuclei DAST scan runs weekly on `main` rather than per pull request, so it does not gate your PR.

## Code of Conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
