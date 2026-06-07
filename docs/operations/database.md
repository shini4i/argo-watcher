# Database

When `STATE_TYPE=postgres` is set, Argo Watcher persists task data in PostgreSQL. The in-memory backend is fine for dev, but production deployments should use Postgres so tasks survive restarts and the server can scale horizontally.

## Schema overview

There is a single table, `tasks`, that stores every deployment task and its status. Indexes are tuned for the two access patterns the Web UI uses: listing recent tasks and looking up a task by ID.

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` (default `gen_random_uuid()`) | Primary key, also unique-indexed via `idx_tasks_id`. |
| `created` | `timestamptz NOT NULL` | Indexed via `idx_tasks_created_app` (descending, with `app`). |
| `updated` | `timestamptz NOT NULL` | Last status transition. |
| `images` | `jsonb NOT NULL` | Image list submitted with the task. |
| `status` | `varchar(20) NOT NULL` | One of: pending, in progress, deployed, failed. |
| `status_reason` | `text` | Human-readable failure reason; empty on success. |
| `app` | `varchar(255) NOT NULL` | Argo CD application name. |
| `author` | `varchar(255) NOT NULL` | Deployment author identifier. |
| `project` | `varchar(255) NOT NULL` | Business project identifier. |

## Migrations

Schema migrations live under [`db/migrations/`](https://github.com/shini4i/argo-watcher/tree/main/db/migrations) and are managed with [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate). Filenames follow the `NNNNNN_description.{up,down}.sql` convention.

### With the Helm chart

The Helm chart bundles an init container that runs migrations on every release. There is nothing for you to do — `helm upgrade` is enough.

### With Docker Compose (development)

The bundled `docker-compose.yml` runs the official `migrate/migrate` image as a one-shot service:

```bash
docker compose up migrations
```

### Manual

When managing the database yourself, run migrations directly with the `migrate` CLI:

```bash
migrate \
  -path db/migrations \
  -database "postgres://watcher:watcher@db.example.com:5432/watcher?sslmode=disable" \
  up
```

Use `down 1` to roll back the most recent migration. Down migrations exist for every applied change.

!!! warning
    Always back up the database before applying down migrations in production. Several down migrations drop indexes and revert column types — they are designed to be safe but a backup turns "almost certainly safe" into "definitely safe".

## Backups

Argo Watcher's data is small and infrequently updated, so a logical dump is more than sufficient.

```bash
pg_dump --no-owner --no-acl \
  -h db.example.com -U watcher watcher \
  | gzip > "argo-watcher-$(date -u +%Y%m%d).sql.gz"
```

A daily dump retained for 30 days is a sensible baseline. If the deployment volume is high or you treat the audit history as critical, increase retention rather than frequency — there's no point dumping more than once an hour given the workload.

Restore with `psql` after recreating the database:

```bash
gunzip -c argo-watcher-20261015.sql.gz | psql -U watcher watcher
```

## Sizing guidance

The `tasks` table grows linearly with the number of deployments. Each row is small (~500 bytes including JSON image data), so storage is rarely the bottleneck — but if your CI fleet is large you should plan for it.

| Deploys/day | Annual rows | Annual table size |
|---|---:|---:|
| 100 | ~36 K | ~20 MB |
| 1 000 | ~365 K | ~200 MB |
| 10 000 | ~3.7 M | ~2 GB |

If the table grows past a few million rows and the Web UI feels slow, consider archiving rows older than your retention window into a separate table. There is no built-in retention job today.

## Where to look next

- **[Observability](observability.md)** — Metrics emitted by the server, including task throughput.
- **[Troubleshooting](troubleshooting.md)** — Common operational issues.
