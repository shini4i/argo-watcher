# Observability

The server exposes Prometheus metrics at `/metrics`, on the same port as the API. With the Helm chart, set `podMonitor.enabled: true` to have Prometheus Operator scrape it; otherwise add a target:

```yaml
scrape_configs:
  - job_name: argo-watcher
    static_configs:
      - targets: ["argo-watcher.example.com:8080"]
```

## Metrics

Twelve metrics, all defined in [`internal/prometheus/metrics.go`](https://github.com/shini4i/argo-watcher/blob/main/internal/prometheus/metrics.go), plus the standard Go runtime ones (`go_*`, `process_*`).

| Metric | Type | Labels | Description |
|---|---|---|---|
| `failed_deployment` | gauge | `app` | Failed deployments since that application's last success; reset to 0 on success. Includes deployments aborted because Argo CD was unreachable. |
| `processed_deployments` | counter | `app` | Deployments processed since startup. |
| `in_progress_tasks` | gauge | | Tasks between submission and a terminal state. |
| `argocd_unavailable` | gauge | | `1` while the Argo CD API is unreachable. |
| `state_unavailable` | gauge | | `1` while the state backend (database) is unreachable. |
| `deployment_duration_seconds` | histogram | `app` | End-to-end time of a **successful** deployment, from the start of monitoring to `deployed`. Failures are excluded: their duration is just the timeout. |
| `argocd_refresh_duration_seconds` | histogram | `app` | Argo CD application refresh requests. Recorded only when the status check asks for a refresh. |
| `gitops_writeback_duration_seconds` | histogram | `app` | Time the write-back held the per-repo lock: clone, commit, push, retries and backoff. |
| `gitops_lock_wait_duration_seconds` | histogram | `app` | Time spent waiting for that lock. High values mean write-backs are queued behind each other. |
| `gitops_batch_size` | histogram | | Applications coalesced into one batch flush. Only with `GIT_BATCH_WRITEBACK`; clustered at `1` means no contention to collapse. |
| `gitops_writeback_skipped_unvalidated` | counter | `app` | Deployments of a `argo-watcher/managed` application whose task carried no valid credential, so the tag was never committed. Any non-zero value is a misconfiguration. |
| `unauthenticated_reads` | counter | `path`, `app` | Reads served without a credential on the endpoints left open while OIDC is enabled (currently `GET /api/v1/tasks/{id}`). |

!!! note "Why some series say `app="unknown"`"
    `POST /api/v1/tasks` accepts a task without a credential, and the application name is free text — so a name arriving that way is labelled `unknown` rather than becoming its own series, which would let anyone reaching the endpoint create unbounded series. This affects `processed_deployments`, `unauthenticated_reads`, and `failed_deployment` when the failure precedes Argo CD confirming the application exists. Deployments submitted with a credential are labelled with their real application throughout.

    The same openness means anyone who can reach the endpoint and knows a managed application's name can raise `gitops_writeback_skipped_unvalidated`. Treat a rise as "investigate", not automatically "our pipeline regressed".

!!! note "Aggregate the gauges across replicas"
    `failed_deployment` and `in_progress_tasks` count what one process saw. Each
    deployment is monitored by a single replica, so a failing application reads as
    healthy on every replica that did not handle it, and the backlog on any one pod
    is only its own share. Aggregate before alerting — `max by (app)` for the former,
    `sum` for the latter, as the examples below do. The aggregation is harmless on a
    single replica. See [High Availability](high-availability.md#known-limits).

The cached reachability behind `argocd_unavailable` and `state_unavailable` is also readable at `GET /api/v1/reachability`, which returns `{"available":bool,"reason":"argocd"|"database"|"both"}` (`reason` omitted when available). The Web UI's banner uses `reason` to name what is down.

## Suggested alerts

Thresholds are starting points; tune them.

```yaml
groups:
  - name: argo-watcher
    rules:
      - alert: ArgoWatcherCDUnreachable
        expr: argocd_unavailable == 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: Argo Watcher cannot reach the Argo CD API
          description: No task will progress until connectivity is restored.

      - alert: ArgoWatcherStateUnreachable
        expr: state_unavailable == 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: Argo Watcher cannot reach its state backend
          description: |
            Pods are unready and, with STATE_TYPE=postgres, deploy requests are
            rejected as locked because the lock state cannot be read.

      - alert: ArgoWatcherFailingDeployments
        expr: max by (app) (failed_deployment) > 3
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.app }} has failed {{ $value }} times in a row"
          description: Check the most recent task in the Web UI for the failure reason.

      - alert: ArgoWatcherTaskBacklog
        expr: sum(in_progress_tasks) > 50
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: Task backlog growing on argo-watcher
          description: Check Argo CD sync performance and DEPLOYMENT_TIMEOUT.

      - alert: ArgoWatcherSlowRefresh
        expr: histogram_quantile(0.95, sum by (app, le) (rate(argocd_refresh_duration_seconds_bucket[10m]))) > 30
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.app }} Argo CD refresh is slow"
          description: |
            A refresh that never settles (an app with a constantly-reconciling
            CronJob, for example) stalls the deployment check. Set TASK_REFRESH
            (or ARGO_REFRESH_APP) to false for such applications.

      - alert: ArgoWatcherSlowGitWriteback
        expr: histogram_quantile(0.95, sum by (app, le) (rate(gitops_writeback_duration_seconds_bucket[10m]))) > 60
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.app }} git write-back is slow"
          description: |
            Points to push contention or a repository with a very large working
            tree (history depth is not a factor — clone and fetch are shallow).
            Compare gitops_lock_wait_duration_seconds for the same app.

      - alert: ArgoWatcherWritebackSkipped
        expr: increase(gitops_writeback_skipped_unvalidated[1h]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.app }} deploys without git write-back"
          description: |
            The application is annotated for write-back but its deployments carry
            no valid credential, so the tag is never committed. Check that the
            pipeline sets ARGO_WATCHER_DEPLOY_TOKEN or BEARER_TOKEN, and that
            nothing redirects the client to a different host.
```

## Example dashboard

A ready-made Grafana dashboard lives at [`monitoring/grafana/dashboards/argo-watcher.json`](https://github.com/shini4i/argo-watcher/blob/main/monitoring/grafana/dashboards/argo-watcher.json). Its **Overview** row covers availability, in-progress tasks, deployment counts and failing apps; the **Per-Application Breakdown** row is driven by an `Application` template variable and shows that app's deployment counts, failures, end-to-end duration, and the refresh / write-back / lock-wait percentiles. `gitops_batch_size` and `state_unavailable` have no panel yet.

Import it through **Dashboards → New → Import**, or run it next to the dev server:

```bash
docker compose --profile monitoring up
```

That starts Prometheus (scraping the dev `backend`) and Grafana with the datasource and dashboard provisioned, at <http://localhost:3001> — anonymous admin, no login. Drive a few deployments through the client to fill the panels.

![Argo Watcher Grafana dashboard](https://raw.githubusercontent.com/shini4i/assets/main/src/argo-watcher/grafana-dashboard.png)

The **Git Write-back Duration** and **Git Lock Wait Duration** panels only fill for applications using write-back (`argo-watcher/managed`); they stay empty for status-only deployments.

### Useful queries

```promql
sum(in_progress_tasks)                          # live workload
topk(5, max by (app) (failed_deployment))       # which apps need attention
sum(rate(processed_deployments[1h])) by (app)    # deployment frequency per app
```

## Tracking the read-auth migration

With [OIDC](../guides/oidc.md) enabled the browser-facing reads require a credential, but `GET /api/v1/tasks/{id}` stays open by default: the client polls it throughout every deployment, and a client too old to send a credential there would fail every deployment it drives. `unauthenticated_reads` measures how much of the fleet is still in that state:

```promql
sum(rate(unauthenticated_reads[1h])) by (app)
```

Each application named is a pipeline running such a client; group by `path` for the endpoint breakdown. When the figure reaches zero and stays there across a full deployment cycle for every project, nothing depends on the exemption and you can close it with `OIDC_REQUIRE_TASK_READ_AUTH=true` — see [Closing the task lookup](../guides/oidc.md#closing-the-task-lookup).

Two caveats: the counter records that a credential was *present*, not that it was valid, and once the endpoint is closed the counter stops moving and its series disappears from `/metrics` after the next restart — which matters if you alert on it with `absent()`.

Nothing is logged per read; the endpoint is polled continuously during a deployment, so a line per read would bury everything else.
