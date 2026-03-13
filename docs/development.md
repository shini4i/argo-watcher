# Development

This guide covers setting up a local development environment for Argo Watcher.

## Prerequisites

- **Go 1.25+** (the project uses toolchain `go1.25.4`)
- **Node.js 20+** (for frontend development)
- **Docker** and **Docker Compose** (for running dependencies locally)
- **[Task](https://taskfile.dev/)** (task runner, replaces Make)
- **[pre-commit](https://pre-commit.com/)** (Git hooks for code quality)

Install the Git hooks:

```bash
pre-commit install
```

Install Go tooling (mock generator, swagger, migration tool):

```bash
task install-deps
```

## Task Runner

The project uses [Task](https://taskfile.dev/) as a build and automation tool. All common development operations are defined in `Taskfile.yml`.

### Available Tasks

| Task                | Description                                              |
|---------------------|----------------------------------------------------------|
| `task install-deps` | Install Go development tools (swag, mockgen, migrate)    |
| `task mocks`        | Generate mock interfaces for unit tests                  |
| `task docs`         | Generate the Swagger JSON spec                           |
| `task test`         | Run the full test suite (generates mocks and docs first) |
| `task build`        | Build the Go binary                                      |
| `task build-ui`     | Build the React frontend bundle                          |
| `task lint-web`     | Lint the React frontend code                             |
| `task test-web`     | Run React frontend unit tests                            |
| `task bootstrap`    | Start all Docker Compose services                        |
| `task teardown`     | Stop all Docker Compose services                         |

Run any task with:

```bash
task <task-name>
```

## Quick Start with Docker Compose

The fastest way to get a full development environment running:

```bash
task bootstrap
```

This starts PostgreSQL, runs database migrations, and prepares the backend and frontend services via Docker Compose.

To stop everything:

```bash
task teardown
```

## Back-End Development

If you prefer to run services individually without Docker Compose, follow the steps below.

### Generate Mocks and Swagger Spec

Before compiling or testing, generate the required mock classes and swagger spec:

```bash
task mocks
task docs
```

### Start the Mock Argo CD Server

The project includes a mock Argo CD server for local development:

```bash
cd cmd/mock
go run .
```

This starts a mock server on port `8081` that simulates the Argo CD API.

### Start Argo Watcher (In-Memory Mode)

For development without a database:

```bash
cd cmd/argo-watcher
go mod download
LOG_LEVEL=debug ARGO_URL=http://localhost:8081 ARGO_TOKEN=example STATE_TYPE=in-memory go run .
```

### Start Argo Watcher (PostgreSQL Mode)

Start the database and run migrations:

```bash
docker compose up -d postgres migrations
```

Then start the server:

```bash
cd cmd/argo-watcher
go mod tidy
LOG_LEVEL=debug \
  ARGO_URL=http://localhost:8081 \
  ARGO_TOKEN=example \
  STATE_TYPE=postgres \
  DB_USER=watcher \
  DB_PASSWORD=watcher \
  DB_NAME=watcher \
  go run .
```

## Front-End Development

```bash
cd web
npm install
npm start
```

The development server opens at [http://localhost:3000](http://localhost:3000).

## Running Tests

### Full Test Suite

Run all backend tests (this also generates mocks and swagger docs automatically):

```bash
task test
```

### Backend Unit Tests Only

```bash
cd cmd/argo-watcher
go test -v ./...
```

To run a specific test suite:

```bash
go test -v -run TestArgoStatusUpdaterCheck ./...
```

### Frontend Tests

```bash
task test-web
```

### Frontend Linting

```bash
task lint-web
```

## Swagger Documentation

The Swagger spec is generated from Go annotations in the source code using [swag](https://github.com/swaggo/swag).

To regenerate the spec:

```bash
task docs
```

This outputs `web/public/swagger/swagger.json`. During `task build-ui`, Vite copies the Swagger UI assets alongside the spec into `web/dist/swagger/`.

!!! note
    Regenerate the spec whenever API annotations, request/response models, or documented routes change.

Once the server is running, the Swagger UI is accessible at:

```text
http://localhost:8080/swagger/index.html
```

For a summary of available API endpoints, see the [API Reference](api.md) page.

## Project Structure

```text
cmd/
  argo-watcher/     # Main server binary
  client/           # CLI client binary
  mock/             # Mock Argo CD server for development
db/
  migrations/       # PostgreSQL migration files
docs/               # MkDocs documentation source
internal/
  argocd/           # Argo CD API client
  auth/             # Authentication (JWT, deploy token)
  helpers/          # Shared utility functions
  lock/             # Deployment lock logic
  migrate/          # Database migration runner
  models/           # Data models
  notifications/    # Webhook notification sender
  server/           # HTTP server and routes
  state/            # State management (in-memory and PostgreSQL)
pkg/
  client/           # CLI client logic
  updater/          # GitOps updater logic
web/                # React/TypeScript frontend
```
