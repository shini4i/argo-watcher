# Quick Start

Get a running Argo Watcher instance in five minutes. This walkthrough uses the project's bundled Docker Compose stack, which includes the server, a Postgres database, the Web UI, and a mock Argo CD service so you can exercise the full task lifecycle without a real Argo CD cluster.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Git](https://git-scm.com/) for cloning the repository
- [`curl`](https://curl.se/) (or any HTTP client)

## 1. Start the stack

Clone the repository and bring everything up:

```bash
git clone https://github.com/shini4i/argo-watcher.git
cd argo-watcher
docker compose up
```

Compose starts five services:

- `postgres` — Storage backend for tasks.
- `migrations` — Applies the database schema.
- `mock` — Stand-in for the Argo CD API on port `8081`.
- `backend` — The Argo Watcher server on port `8080`.
- `frontend` — The Web UI on port `3100`.

The first run takes a couple of minutes to compile the Go binaries. Subsequent restarts are much faster.

## 2. Verify the server is healthy

In a new terminal:

```bash
curl -s http://localhost:8080/healthz
```

You should see a `200 OK` response with health details for the database and Argo CD client.

## 3. Submit a task

Create a task that asks Argo Watcher to verify a particular image is deployed:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d @- <<'EOF'
--8<-- "quick-start/task.json"
EOF
```

The server responds with `202 Accepted` and the task ID:

```json
{ "id": "...", "status": "accepted" }
```

## 4. Watch the task transition states

Poll the task to see it transition through its lifecycle:

```bash
curl -s "http://localhost:8080/api/v1/tasks/<task-id>" | jq
```

Because the mock Argo CD service simulates a successful deployment, the task progresses from `in progress` to `deployed` within a few seconds. Refer to [Concepts → Task Lifecycle](concepts.md#task-lifecycle) for an explanation of each state.

## 5. Open the Web UI

Visit [http://localhost:3100](http://localhost:3100) to see the dashboard. You should find your task listed, along with its current status and timing details.

## Where to next

- **[Installation](../guides/install.md)** — Deploy Argo Watcher to a real Kubernetes cluster.
- **[Concepts](concepts.md)** — Understand how the components fit together.
- **[API Reference](../reference/api.md)** — Explore the full HTTP API.
