# Frontend End-to-End Tests

The React-admin frontend now ships with a Playwright suite that exercises the UI against the docker-compose stack (API, Postgres, mock Argo, Vite dev server). Use this flow when you need a confidence boost before shipping UI or API changes.

## Prerequisites

- Docker + docker-compose for bootstrapping Postgres/backend/mock services (`task bootstrap`).
- Node 20+ (handled automatically by `flake.nix` + `direnv allow`).
- Ports `5432`, `8080`, `8081`, and `3100` must be free locally.

## Commands

- `task e2e` — boots the compose stack (unless `E2E_SKIP_BOOTSTRAP=1`), runs `npm run test:e2e` inside `web/`, then tears the stack down unless `E2E_KEEP_STACK=1`.
- `npm run test:e2e` (from `web/`) — runs all specs against whatever stack is already running.
- `npm run test:e2e:ui` — opens the Playwright UI runner for iterative debugging.
- `npm run test:e2e:report` — re-opens the most recent HTML report stored under `web/e2e/artifacts/report`.

> **Note:** Tests are executed serially (`workers: 1`) because they mutate the shared Postgres state via the API. Running them in parallel would cause truncation/seed races between specs.

Environment overrides:

| Variable | Default | Purpose |
| --- | --- | --- |
| `WEB_BASE_URL` | `http://localhost:3100` | Frontend origin served by Vite in docker-compose |
| `API_BASE_URL` | `http://localhost:8080` | Go API entrypoint |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB` | Compose defaults | Used by the seeding helpers to reset/adjust tasks directly |
| `E2E_SKIP_BOOTSTRAP` | `0` | Skip the automatic `task bootstrap` step |
| `E2E_KEEP_STACK` | `0` | Leave docker-compose running after the suite |

## What the Suite Covers

1. **Recent tasks** — seeds success/failure/in-progress deployments and asserts datagrid rendering + expandable status reasons.
2. **History filters + exports** — backdates data, exercises date/application filters, and verifies anonymized JSON downloads.
3. **Deploy lock + config drawer** — toggles theme/timezone preferences, flips the deploy lock via REST/WebSocket, and validates the bottom banner.
4. **Task details** — deep-links into `/task/:id`, validates metadata/timeline/status reason, and captures an `Open in Argo CD UI` action-state snapshot.
5. **Empty & failure states** — verifies the “No recent tasks” placeholder auto-refresh message and forces `/api/v1/config` failures to assert the network error notification.
6. **Status fidelity** — seeds tasks with the canonical status strings (`deployed`, `failed`, `in progress`, `aborted`) defined in `internal/models/constants.go` so UI badges match backend expectations.

Every scenario attaches full-page screenshots, traces (on failure), and downloads to the Playwright report under `web/e2e/artifacts`.
