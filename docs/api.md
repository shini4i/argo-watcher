# API Reference

Argo Watcher exposes a REST API for managing deployment tasks, querying status, and controlling the deployment lock. The API is served by the Argo Watcher server, typically at port `8080`.

!!! tip
    A full interactive API reference is available via the built-in Swagger UI at `/swagger/index.html` on your Argo Watcher server.

## Base URL

All API endpoints are prefixed with `/api/v1` unless otherwise noted.

```text
https://argo-watcher.example.com/api/v1
```

## Authentication

If the [Git Integration](git-integration.md) is enabled, task creation requires one of the following:

- **Deploy token** -- Pass the `ARGO_WATCHER_DEPLOY_TOKEN` value as a query parameter or header.
- **JWT token** -- Include a `Bearer` token in the `Authorization` header.

If [Keycloak](keycloak.md) is enabled, additional endpoints (such as deploy lock) require a valid Keycloak session.

## Endpoints

### Create a Task

Submit a new deployment monitoring task.

```text
POST /api/v1/tasks
```

**Request body:**

```json
{
  "app": "my-app",
  "author": "John Doe",
  "project": "example",
  "images": [
    {
      "image": "ghcr.io/shini4i/argo-watcher",
      "tag": "v0.8.0"
    }
  ]
}
```

| Field     | Type      | Required | Description                                     |
|-----------|-----------|----------|-------------------------------------------------|
| `app`     | `string`  | Yes      | Argo CD application name                        |
| `author`  | `string`  | Yes      | Person who triggered the deployment             |
| `project` | `string`  | Yes      | Business project identifier                     |
| `images`  | `[]Image` | Yes      | List of images with expected tags               |

**Response (202 Accepted):**

```json
{
  "status": "accepted",
  "id": "be8c42c0-a645-11ec-8ea5-f2c4bb72758a"
}
```

**Response (406 Not Acceptable):**

Returned when the task is rejected (e.g., deployment lock is active or invalid payload).

```json
{
  "status": "rejected",
  "error": "lockdown is active, deployments are not accepted"
}
```

**Response (500 Internal Server Error):**

Returned when token validation fails due to an internal error.

**Response (503 Service Unavailable):**

Returned when the server cannot process the task (e.g., Argo CD is unreachable).

```json
{
  "status": "down",
  "error": "error details"
}
```

---

### List Tasks

Retrieve tasks matching the specified filters.

```text
GET /api/v1/tasks
```

**Query parameters:**

| Parameter        | Type     | Required | Description                                      |
|------------------|----------|----------|--------------------------------------------------|
| `app`            | `string` | No       | Filter by application name                       |
| `from_timestamp` | `number` | No       | Start of time range (Unix timestamp)             |
| `to_timestamp`   | `number` | No       | End of time range (Unix timestamp, defaults to now) |
| `limit`          | `int`    | No       | Maximum number of tasks to return                |
| `offset`         | `int`    | No       | Number of tasks to skip (for pagination)         |

**Example:**

```bash
curl "https://argo-watcher.example.com/api/v1/tasks?from_timestamp=1648390029&limit=10"
```

---

### Get Task Status

Retrieve the current status of a specific task.

```text
GET /api/v1/tasks/{id}
```

**Path parameters:**

| Parameter | Type     | Description        |
|-----------|----------|--------------------|
| `id`      | `string` | Task UUID          |

**Response (200 OK):**

```json
{
  "status": "deployed"
}
```

Possible status values: `accepted`, `in progress`, `deployed`, `failed`, `app not found`, `timed out`.

**Response (404 Not Found):**

```json
{
  "status": "task not found"
}
```

---

### Get Server Version

```text
GET /api/v1/version
```

Returns the server version as a plain string.

---

### Get Server Configuration

Returns the server configuration, excluding sensitive values such as tokens and passwords.

```text
GET /api/v1/config
```

---

### Health Check

Check if the server is ready to accept new tasks.

```text
GET /healthz
```

!!! note
    This endpoint is not under the `/api/v1` prefix.

**Response (200 OK):**

```json
{
  "status": "healthy"
}
```

**Response (503 Service Unavailable):**

```json
{
  "status": "unhealthy"
}
```

---

### Deploy Lock

Manage the deployment lock. See [Deployment Locking](git-integration.md#deployment-locking) for details.

**Set lock:**

```text
POST /api/v1/deploy-lock
```

**Release lock:**

```text
DELETE /api/v1/deploy-lock
```

**Check lock status:**

```text
GET /api/v1/deploy-lock
```

Returns `true` if the lock is active, `false` otherwise.

## Swagger UI

The Argo Watcher server bundles a Swagger UI that provides an interactive API explorer. Access it at:

```text
https://argo-watcher.example.com/swagger/index.html
```

The Swagger spec is auto-generated from Go source code annotations. See the [Development](development.md#swagger-documentation) guide for instructions on regenerating the spec.
