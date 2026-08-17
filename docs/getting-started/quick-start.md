# Quick Start

Run Argo Watcher locally in five minutes. The bundled Docker Compose stack includes the server, Postgres, the Web UI, and a mock Argo CD, so you can drive a full task lifecycle without a cluster.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Git](https://git-scm.com/)
- [`curl`](https://curl.se/) and [`jq`](https://jqlang.github.io/jq/)

## 1. Start the stack

```bash
git clone https://github.com/shini4i/argo-watcher.git
cd argo-watcher
docker compose up
```

Five services come up: `postgres` (task storage), `migrations` (schema), `mock` (a stand-in Argo CD API on `8081`), `backend` (the server on `8080`), and `frontend` (the Web UI on `3100`).

The first run spends a couple of minutes compiling the Go binaries.

## 2. Check the server is healthy

```bash
curl -s http://localhost:8080/readyz
```

`{"status":"up"}` means the server is serving and its state backend is reachable. See [Health and probe endpoints](../reference/api.md#health-and-probe-endpoints) for the difference between `/readyz` and `/livez`.

## 3. Submit a task

The mock Argo CD knows three applications — `app`, `app2` and `app4` — and reports `app` as synced, healthy, and running `app:v0.0.1`. Ask Argo Watcher to confirm exactly that:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d @- <<'EOF'
--8<-- "quick-start/task.json"
EOF
```

The server answers `202 Accepted` with the task id:

```json
{ "id": "...", "status": "accepted" }
```

## 4. Watch the task reach its final state

```bash
curl -s "http://localhost:8080/api/v1/tasks/<task-id>" | jq
```

The task starts as `in progress` and becomes `deployed` within a few seconds. Submit one for an application the mock does not know and it ends as `app not found` instead — [Concepts → Task Lifecycle](concepts.md#task-lifecycle) explains each state.

## 5. Open the Web UI

Visit [http://localhost:3100](http://localhost:3100). Your task is listed with its status and timings.

## Where to next

- **[Installation](../guides/install.md)** — deploy to a real Kubernetes cluster.
- **[Concepts](concepts.md)** — how the components fit together.
- **[API Reference](../reference/api.md)** — the full HTTP API.
