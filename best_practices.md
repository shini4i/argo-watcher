# Best Practices

Coding standards for argo-watcher. PR-Agent's `improve` tool uses this file as a
reference and flags violations with the `Organization best practice` label.

## Go — general

- Keep it simple. Choose the most direct solution that works; avoid abstraction
  layers, interfaces, or configurability that no current caller needs.
- Every exported type, function, and method has a doc comment that starts with
  its name and describes behaviour, not restated signature. Update the comment
  when the implementation changes; never drop it.
- Return wrapped errors (`fmt.Errorf("...: %w", err)`) instead of swallowing
  them into empty results or generic strings that hide the cause.
- Use the project's libraries, not stdlib equivalents: gin for HTTP handlers,
  zerolog for logging (never `fmt.Println`/`log`), gorm for DB access,
  caarlos0/env + validator tags for configuration.

## Concurrency

- Any access to shared mutable state (the WebSocket `connections`/`closedConns`
  registry, lockdown `ManualLock`/`OverrideMode`, in-memory task slice) must
  hold the correct mutex; take an RLock for reads and a full lock for writes.
- Never write to a WebSocket connection after it has been closed; check the
  closed set under the lock.
- Goroutines started for background work (heartbeats, schedule watchers,
  rollout waiters, obsolete-task cleanup) must observe the shutdown channel and
  exit cleanly; do not leak goroutines on server shutdown.
- Prefer passing values into goroutines over closing over loop variables.

## Security

- Secret-bearing config fields (`ArgoToken`, `DeployToken`, `JWTSecret`, DB DSN,
  webhook token) must carry the `json:"-"` tag so they never appear in the
  `GET /api/v1/config` response.
- Compare secrets and tokens in constant time (`crypto/subtle`), not with `==`.
- Validate any externally-influenced outbound URL (Keycloak, ArgoCD, webhook)
  for scheme/host before use to prevent SSRF.
- State-mutating handlers (`POST /api/v1/tasks`, deploy-lock endpoints) must run
  the lockdown and auth checks before acting.
- Keep `#nosec` annotations together with the justification comment that
  explains why the finding is acceptable; do not add new suppressions without
  one.

## Database

- Honour `RowsAffected` on `UPDATE`/`DELETE` to detect no-op writes instead of
  assuming success.
- Thread a `context.Context` through DB calls where a request or loop context is
  available; do not hardcode `context.Background()` in request-scoped paths.

## Testing

- Follow TDD: write the failing test before the implementation for new features
  and bug fixes.
- Use testify for assertions and go.uber.org/mock (mockgen) for mocks; prefer
  table-driven tests.
- Regenerate mocks with `task mocks` from their interface sources; never
  hand-edit files under `*/mock/`.

## Frontend (web/)

- TypeScript is in strict mode with the native TS 7 toolchain; keep types sound
  and avoid `any`. Lint with oxlint (`npm run lint`), not ESLint.
- Colocate a `.test.tsx` with each source file and cover new behaviour with
  Vitest + @testing-library/react.
