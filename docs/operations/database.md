# Database

When `STATE_TYPE=postgres` is set, Argo Watcher persists task data in PostgreSQL. The in-memory backend is fine for dev, but production deployments should use Postgres so tasks survive restarts and the server can scale horizontally.

## Schema overview

There are two tables. `tasks` stores every deployment task and its status; indexes are tuned for the two access patterns the Web UI uses: listing recent tasks and looking up a task by ID.

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` (default `gen_random_uuid()`) | Primary key, also unique-indexed via `idx_tasks_id`. |
| `created` | `timestamptz NOT NULL` | Indexed via `idx_tasks_created_app` (descending, with `app`). |
| `updated` | `timestamptz NOT NULL` | Last status transition. |
| `images` | `jsonb NOT NULL` | Image list submitted with the task. |
| `status` | `varchar(20) NOT NULL` | A task status value defined in `internal/models/constants.go` (e.g. in progress, deployed, failed, cancelled, aborted, app not found). |
| `status_reason` | `text` | Human-readable failure reason; empty on success. |
| `is_rollback` | `boolean NOT NULL DEFAULT false` | `true` when this task's image set was previously deployed for the app. |
| `rollback_target_id` | `text NOT NULL DEFAULT ''` | ID of the earlier task this deployment rolls back to; empty when not a rollback. |
| `validated` | `boolean NOT NULL DEFAULT false` | `true` when the request that created the task presented a valid credential. Gates the git write-back and what the task may supersede; never exposed through the API. |
| `timeout` | `int NOT NULL DEFAULT 0` | Per-task rollout deadline in seconds; `0` when the client did not override `DEPLOYMENT_TIMEOUT`. |
| `refresh` | `boolean` | Per-task override of `ARGO_REFRESH_APP`; `NULL` when the client omitted it, which is distinct from an explicit `false`. |
| `owner_id` | `text` | The replica currently monitoring the rollout; `NULL` when unclaimed. See [High Availability](high-availability.md#task-ownership). |
| `lease_expires_at` | `timestamptz` | When that claim lapses. A lapsed claim on an in-progress task is taken over by another replica; indexed for that sweep via the partial index `idx_tasks_claimable`. |
| `app` | `varchar(255) NOT NULL` | Argo CD application name. |
| `author` | `varchar(255) NOT NULL` | Deployment author identifier. |
| `project` | `varchar(255) NOT NULL` | Business project identifier. |

`deploy_lock` holds the [manual deploy lock](../guides/deployment-lock.md#manual-lockdown) so it applies to every replica and survives restarts. It is seeded by the migration and always holds exactly one row.

| Column | Type | Notes |
|---|---|---|
| `id` | `int` | Primary key, pinned to `1` by a `CHECK` constraint so a second row cannot exist. |
| `manual_lock` | `boolean NOT NULL DEFAULT false` | `true` while an operator-set lockdown is active. |
| `override_until` | `timestamptz` | Deadline of a temporary override of a scheduled lockdown; `NULL` when none is active. |

`schema_compatibility` records the oldest build allowed to run against the current schema, so a rolled-back release refuses to start rather than failing on something a later migration removed. See [Rolling back a release](#rolling-back-a-release). Like `deploy_lock` it always holds exactly one row.

| Column | Type | Notes |
|---|---|---|
| `id` | `int` | Primary key, pinned to `1` by a `CHECK` constraint so a second row cannot exist. |
| `min_bundled_version` | `int NOT NULL DEFAULT 0` | Migration version of the oldest build this schema tolerates; `0` while no migration has removed anything. |

## Migrations

Schema migrations live under [`db/migrations/`](https://github.com/shini4i/argo-watcher/tree/main/db/migrations) and are managed with [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate). Filenames follow the `NNNNNN_description.{up,down}.sql` convention.

### With the Helm chart

The Helm chart runs migrations automatically as a `pre-install`/`pre-upgrade` hook Job that executes `argo-watcher --migrate`. There is nothing for you to do — `helm install`/`helm upgrade` is enough.

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

### Rolling back a release

Migrations are **forward-only**: rolling a release back does not roll the schema back. The older binary starts against the newer schema and ignores the columns it does not know about. Its `--migrate` run notices the database is ahead of the migrations it ships, logs a warning, and applies nothing.

That works because migrations only ever add. When one genuinely has to remove something an older build reads, it records the oldest build the resulting schema still tolerates, as a migration version:

```sql
ALTER TABLE tasks DROP COLUMN legacy_field;
UPDATE schema_compatibility
   SET min_bundled_version = GREATEST(min_bundled_version, 11);
```

A build whose newest bundled migration is below that number refuses to start and names both versions, rather than starting and then failing on a column that is gone. Roll forward to a release at or above it instead.

`task lint-migrations` fails any migration that removes something without recording a floor, so the two cannot drift apart.

!!! warning
    The `down` migrations exist for local development against a scratch database. They are not a supported production operation, and the first three are empty files — `migrate down` below version 4 reports success while leaving the tables in place.

## Backups

Argo Watcher's data is small and infrequently updated, so a logical dump is more than sufficient.

```bash
pg_dump --no-owner --no-acl \
  -h db.example.com -U watcher watcher \
  | gzip > "argo-watcher-$(date -u +%Y%m%d).sql.gz"
```

A daily dump retained for 30 days is a sensible baseline. If you treat the deployment history as an audit trail, increase retention rather than frequency.

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

## Retention

By default every task is kept forever. Setting `TASK_RETENTION_ENABLED=true` turns on a sweep that deletes finished tasks created longer ago than `TASK_RETENTION_DAYS` (365 by default, between 1 and 36500). It runs with the hourly obsolete-task sweep, in batches of 1 000 rows, so enabling it on a table holding years of history does not lock the table for the duration.

Two kinds of task are never deleted, however old. One still in progress, since a replica may be monitoring it and would then write a status for a row that no longer exists. And one under an unexpired lease, whatever its status, because a rollout resumed after an outage is old but still live. Such a task is collected by a later sweep, once its lease lapses. The setting only applies to `STATE_TYPE=postgres`; with the in-memory backend it is inert and the server logs a warning at startup.

!!! warning
    Deleted history is gone: the rows back the Web UI's task list and any audit trail you keep. Take a dump before the first sweep, and if the deployment history is an audit record, set the window to match your retention policy rather than leaving the default.

## Where to look next

- **[Observability](observability.md)** — Metrics emitted by the server, including task throughput.
- **[Troubleshooting](troubleshooting.md)** — Common operational issues.
