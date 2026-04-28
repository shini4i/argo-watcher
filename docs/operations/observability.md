# Observability

The Argo Watcher server exposes Prometheus metrics on the same port as the API (default `8080`) at the `/metrics` endpoint. Scrape it like any other Prometheus target:

```yaml
scrape_configs:
  - job_name: argo-watcher
    static_configs:
      - targets: ["argo-watcher.example.com:8080"]
    metrics_path: /metrics
```

## Exposed metrics

The server emits four metrics today, all defined in [`cmd/argo-watcher/prometheus/metrics.go`](https://github.com/shini4i/argo-watcher/blob/main/cmd/argo-watcher/prometheus/metrics.go).

| Metric | Type | Labels | Description |
|---|---|---|---|
| `failed_deployment` | gauge | `app` | Per-application failed deployment count since the last success. Reset to 0 when an application deploys successfully. |
| `processed_deployments` | counter | `app` | Total deployments processed since server startup, per application. |
| `argocd_unavailable` | gauge | (none) | `1` when Argo Watcher cannot reach the Argo CD API, `0` otherwise. |
| `in_progress_tasks` | gauge | (none) | Number of tasks currently in progress (between submission and terminal state). |

In addition, the standard Go runtime metrics from the Prometheus client library are exposed (`go_*`, `process_*`).

## Suggested alerts

These alerts cover the failure modes the metrics are designed to catch. Tune the thresholds to your environment.

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
          description: |
            argo_watcher has reported argocd_unavailable=1 for more than 5 minutes.
            Tasks will not progress until connectivity is restored.

      - alert: ArgoWatcherFailingDeployments
        expr: failed_deployment > 3
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.app }} has failed {{ $value }} times in a row"
          description: |
            The application has not deployed successfully for more than 10 minutes.
            Inspect the most recent task in the Web UI for the failure reason.

      - alert: ArgoWatcherTaskBacklog
        expr: in_progress_tasks > 50
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: Task backlog growing on argo-watcher
          description: |
            More than 50 tasks have been in progress for over 15 minutes.
            Check Argo CD sync performance and DEPLOYMENT_TIMEOUT.
```

## Grafana panel queries

Three panels that cover the most useful operational views.

### Active in-progress tasks

```promql
sum(in_progress_tasks)
```

Use a stat or graph panel to monitor live workload. Pair with `argocd_unavailable` on the same dashboard.

### Top 5 apps with consecutive failures

```promql
topk(5, failed_deployment)
```

A bar gauge highlights which applications need attention. Drill into each app via the Argo Watcher Web UI to see the underlying task failures.

### Deployments per hour, per app

```promql
sum(rate(processed_deployments[1h])) by (app)
```

A graph panel showing deployment frequency per application — useful for capacity planning and identifying applications that suddenly stop deploying.

## Where to look next

- **[Troubleshooting](troubleshooting.md)** — Operational runbook keyed off the same symptoms these metrics flag.
- **[Database](database.md)** — Backup, sizing, and migration guidance.
