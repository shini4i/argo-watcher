# Deployment Lock

A deployment lock freezes deploys — for a maintenance window, a release freeze, or an incident. While it is held every `POST /api/v1/tasks` is rejected with `406` and `lockdown is active, deployments are not accepted`. The lock is server-global: it applies to every application, and there is no per-application lock.

There are two ways to hold it: a recurring schedule, and a manual toggle.

## Scheduled lockdown

Set the windows in the chart:

```yaml
scheduledLockdown:
  - "Wed 20:00 - Thu 08:00"
  - "Fri 20:00 - Mon 08:00"
```

or directly, as one comma-separated string:

```yaml
extraEnvs:
  - name: LOCKDOWN_SCHEDULE
    value: "Wed 20:00 - Thu 08:00, Fri 20:00 - Mon 08:00"
```

Deploys are then rejected between Wednesday 20:00 and Thursday 08:00, and between Friday 20:00 and Monday 08:00. The Web UI banner appears and disappears on its own within a few seconds of a window opening or closing; no page refresh is needed.

!!! warning
    Windows are evaluated in the **server's** timezone, which is UTC in the published image unless you set `TZ`.

A window that starts and ends on the same day must end later than it starts. An overnight freeze names the following day — write `Sat 22:00 - Sun 06:00`, not `Sat 22:00 - Sat 06:00`. The server refuses to start on a window it could never open, rather than accepting a freeze that silently never fires.

## Manual lockdown

!!! note
    Manual locking requires OIDC authentication. Without an auth backend the server does not register `POST`/`DELETE /api/v1/deploy-lock` and the Web UI shows no lock toggle. Scheduled lockdown works either way.

### From the Web UI

Click the **Argo Watcher** logo to open the configuration drawer, then use the switch in its **Deploy Lock** section — it reads *Lock engaged* or *Lock released*. Users outside `OIDC_PRIVILEGED_GROUPS` see the switch disabled, with "Deploy lock requires privileged access."

![Deployment lock toggle in the Web UI](https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/deployment-lock.png)

### From the API

Both calls need a token from a user in one of the `OIDC_PRIVILEGED_GROUPS`, in the `Oidc-Authorization` header:

```bash
# Hold the lock
curl -X POST -H "Oidc-Authorization: $TOKEN" \
  https://argo-watcher.example.com/api/v1/deploy-lock

# Release it
curl -X DELETE -H "Oidc-Authorization: $TOKEN" \
  https://argo-watcher.example.com/api/v1/deploy-lock
```

Reading the state — `GET /api/v1/deploy-lock` — needs no privileged group, though with OIDC enabled it does need some credential like the other reads. See [Protected endpoints](oidc.md#protected-endpoints).

### Releasing during a scheduled window

Releasing the lock inside a scheduled window does not cancel the schedule: it **suppresses it for 15 minutes**, after which the window applies again unless it has closed meanwhile. Setting the lock again clears a pending suppression.

## Multiple replicas

With `STATE_TYPE=postgres` the manual lock and its suppression live in the database, so a lock set through any replica rejects deploys on all of them and survives a restart. Enforcement is immediate — every deploy request resolves the lock state at that moment. Web UI clients see the banner change within a few seconds, when their replica next samples the state; the operator who made the change sees it right away.

With `STATE_TYPE=in-memory` the lock lives in the process that served the request. That is correct for a single replica — the only supported configuration for in-memory state — but it is lost on restart.

!!! warning
    If the database cannot be read, the lock state is unknown and deploys are rejected as though a lock were held. Rejecting a deployment during an outage is recoverable; letting one through during a freeze is not. See [All deployments rejected as locked](../operations/troubleshooting.md#all-deployments-rejected-as-locked-but-nobody-set-a-lock).
