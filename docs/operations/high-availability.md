# High Availability

Argo Watcher runs with more than one replica when `STATE_TYPE=postgres`. All
replicas are equal — there is no leader and no election. They share the task
table, the deploy lock, and the write-back serialization, and each deployment is
monitored by exactly one replica at a time.

!!! warning "Postgres is required"
    With `STATE_TYPE=in-memory` every replica has its own private task list, its
    own deploy lock, and no way to take a deployment over. It is correct for a
    single replica only. The Helm chart requires Postgres above `replicaCount: 1`.

## What each replica does

Any replica can accept a deployment. The one that accepts it also monitors it:
it polls Argo CD for the rollout, performs the git write-back, and records the
outcome. The others take no part in that deployment unless the owner stops.

Reads are served by whichever replica the request lands on, from the shared
table, so the task list and task details are identical everywhere.

## Task ownership

Accepting a task claims it. The claim is a lease held in the `tasks` row:
`owner_id` names the replica and `lease_expires_at` is when the claim lapses.
The owner extends it every 10 seconds for as long as it is watching the rollout.

Every replica sweeps for lapsed leases on a 15-second timer and takes over what
it finds. A lease is lapsed when its owner stopped renewing it — because the pod
crashed, was evicted, lost its database connection for long enough, or shut
down. The new owner starts monitoring the rollout from where it stands: it
re-reads the application, re-runs the write-back if the target tag is not
committed yet, and records the outcome.

Two properties make that safe to run everywhere at once. Claims are taken with
`FOR UPDATE SKIP LOCKED`, so simultaneous sweeps partition the abandoned tasks
instead of handing the same one to two replicas. And a replica stops watching a
rollout as soon as it is no longer the one to finish it — because it lost its
claim, which it discovers on its next renewal, or because it has begun shutting
down and is about to release the claim itself. Either way it stops without
writing a status, so it cannot clobber the outcome the new owner records.

### How long a deployment is unattended

| Event | Time before another replica resumes it |
|---|---|
| Graceful shutdown (rolling update, scale-down) | Up to 15s — one sweep |
| Crash, eviction, `SIGKILL` | Up to 45s — the 30s lease plus one sweep |

A graceful shutdown is faster because the last thing a replica does is release
its claims, rather than leaving them to lapse.

### The deadline does not restart

A resumed deployment keeps the window it was accepted with: the remaining time
is measured from when the task was created, not from when it was taken over.
The window is a whole number of poll intervals, so a rollout ends at most one
interval past its `DEPLOYMENT_TIMEOUT` (or its per-task `TASK_TIMEOUT`), and a
resumed one at most two — the remainder it is handed is rounded up to whole polls
again. That overshoot does not grow with further handovers, because every
takeover measures what is left from the moment the task was accepted. One whose
window already elapsed while unattended is marked `aborted` rather than resumed.

## What is shared, and what is not

| Behaviour | Across replicas |
|---|---|
| Task history and status | Shared — one Postgres table. |
| Deploy lock and its schedule override | Shared. See [Deployment Lock](../guides/deployment-lock.md#multiple-replicas). |
| Superseding an in-flight deployment | Shared — a new deployment cancels the older one even when another replica is watching it. |
| Git write-back | Serialized by a Postgres advisory lock, so concurrent write-backs to one repository queue rather than collide. |
| Rollout monitoring | Owned by one replica at a time, handed over as described above. |
| Web UI banners (deploy lock, Argo CD reachability) | Each replica polls the shared state, so clients see a change within a few seconds regardless of which replica they are connected to. |

### Known limits

- **Mattermost threading.** The link between a start post and its result is held
  in the memory of the replica that posted it. A deployment finished by a
  different replica posts its result as a channel message instead of a threaded
  reply. See [Notifications](../guides/notifications.md).
- **Per-replica metrics.** `failed_deployment` and `in_progress_tasks` are
  per-process gauges. Aggregate across pods when alerting — `max by (app)` for
  the former, `sum` for the latter — or a rollout watched by one replica will
  look absent on the others. See [Observability](observability.md).

## Sizing

Replicas exist for availability, not throughput: a single one comfortably
handles the polling load of a normal deployment fleet. Two is the useful number
— it survives a node failure and makes rolling updates transparent. More than
that mainly adds sweeps against the same database.
