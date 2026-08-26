# Development

Setting up a local environment. What a change must satisfy before it can be merged is in [CONTRIBUTING.md](https://github.com/shini4i/argo-watcher/blob/main/.github/CONTRIBUTING.md).

## Prerequisites

- **Go** — the version in `go.mod` is authoritative
- **Node.js 24+** for the frontend
- **Docker** and Docker Compose
- **[Task](https://taskfile.dev/)** — every automation lives in `Taskfile.yml`
- **[pre-commit](https://pre-commit.com/)** — `pre-commit install`

`nix develop` provides all of the above plus the scanners CI runs (`trufflehog`, `gosec`, `govulncheck`, `trivy`, `zizmor`). Without Nix, install `trufflehog` yourself — one pre-commit hook is a secret scan and fails without it. The frontend scanner, retire.js, is a `web/` dev dependency rather than a Nix package, so `task scan-web` provides it via `npm ci`.

Then install the Go tooling (`swag`, `mockgen`, `migrate`):

```bash
task install-deps
```

## Everything with Docker Compose

```bash
task bootstrap    # postgres + migrations + backend + frontend + mock Argo CD
task teardown
```

The Web UI is then on [http://localhost:3100](http://localhost:3100) and the API on `8080`. See the [Quick Start](../getting-started/quick-start.md) for driving a task through it.

## Running the pieces directly

Generated mocks and the Swagger spec are inputs to the build, so create them first:

```bash
task mocks
task docs
```

Start the mock Argo CD (port `8081`):

```bash
go run ./cmd/mock
```

Then the server, in-memory:

```bash
LOG_LEVEL=debug DEV_ENVIRONMENT=true ARGO_URL=http://localhost:8081 ARGO_TOKEN=example \
  STATE_TYPE=in-memory go run ./cmd/argo-watcher
```

Or against Postgres:

```bash
docker compose up -d postgres migrations

LOG_LEVEL=debug DEV_ENVIRONMENT=true ARGO_URL=http://localhost:8081 ARGO_TOKEN=example \
  STATE_TYPE=postgres DB_HOST=localhost DB_PORT=5432 \
  DB_USER=watcher DB_PASSWORD=watcher DB_NAME=watcher \
  go run ./cmd/argo-watcher
```

The frontend dev server, with hot reload on [http://localhost:5173](http://localhost:5173), proxies `/api` and `/ws` to `localhost:8080`:

```bash
cd web
npm install
npm run dev
```

`DEV_ENVIRONMENT=true` above is what makes this work: the dev server is a different
origin from the API, and the server allows no cross-origin request without it.

## Tasks

| Task | Description |
|---|---|
| `task install-deps` | Install `swag`, `mockgen` and `migrate` |
| `task mocks` | Generate the gomock mocks (into the gitignored `internal/mocks/`) |
| `task docs` | Generate the Swagger spec into `web/public/swagger/` |
| `task build` | Build the server binary |
| `task build-ui` | Build the frontend bundle into `web/dist` |
| `task test` | Backend tests (generates mocks and docs first) |
| `task test-integration` | GitOps updater against real Gitea + Toxiproxy, and the Keycloak auth flow (Docker) |
| `task test-web` | Frontend unit tests (Vitest) |
| `task test-web-e2e` | Playwright browser suite against the built UI (Docker) |
| `task lint-web` | Lint the frontend (oxlint) |
| `task scan-web` | Scan the frontend's JavaScript for known-vulnerable libraries (retire.js) |
| `task bootstrap` / `task teardown` | Bring the Compose stack up / down |

`task --list` shows the rest, including the kind-cluster helpers.

## Tests

`task test` needs Postgres on `localhost:5432` with the credentials above — `docker compose up -d postgres migrations` provides exactly that. A single suite:

```bash
go test -v -run TestArgoStatusUpdaterCheck ./...
```

**Integration tests** (`task test-integration`) exercise the GitOps updater against a real Gitea with TCP fault injection through Toxiproxy, plus privileged and unprivileged access to the deploy lock against a real Keycloak. The task brings the `integration` Compose profile up and tears it down afterwards; if a previous run is still up, `docker compose --profile integration down -v` first to avoid port conflicts.

**Browser tests** (`task test-web-e2e`) cover what jsdom cannot: the top-level OIDC redirect and the path it returns to, session recovery across a reload, the server's SPA fallback for deep-linked task URLs, the live WebSocket behind the deploy-lock banner, and the privileged write flows for a privileged versus a regular user. The task builds `web/dist`, brings Keycloak up, and lets Playwright start two servers (`8100` without OIDC, `8101` with) plus the mock Argo CD. Specs live in `web/e2e/`.

**The end-to-end lab** in [`test/e2e/`](https://github.com/shini4i/argo-watcher/tree/main/test/e2e) runs real Argo CD, Gitea and a race-detector build on a kind cluster, once per release. Its README covers the phases and how to add one.

## Swagger spec

The spec is generated from [swag](https://github.com/swaggo/swag) annotations on the handlers — the annotations are the source of truth, so update them in the same commit as a route or model change. `task docs` regenerates `web/public/swagger/swagger.json` (gitignored); `task build-ui` copies the Swagger UI assets next to it into `web/dist/swagger/`. The server then serves the explorer at `/swagger/index.html`.

## Layout

```text
cmd/
  argo-watcher/     # server binary
  client/           # CLI client binary
  mock/             # mock Argo CD for local development
db/migrations/      # PostgreSQL migrations
docs/               # this documentation (MkDocs)
internal/
  argocd/           # Argo CD client, rollout monitoring, git write-back
  auth/             # OIDC, JWT and deploy-token authentication
  client/           # client logic
  config/           # environment-variable configuration
  helpers/          # shared utilities
  lock/             # deployment lock
  logging/          # slog setup
  migrate/          # migration runner
  mocks/            # generated mocks (gitignored)
  models/           # data models
  notifications/    # webhook and Mattermost notifiers
  prometheus/       # metrics
  server/           # HTTP server, routes, WebSocket
  state/            # in-memory and PostgreSQL state
  updater/          # git operations
test/e2e/           # kind-based end-to-end lab
web/                # React/TypeScript frontend
```
