#!/usr/bin/env bash
# Collect + assert soak signals. Fails (exit 1) if any gate trips:
#   - any DATA RACE in an argo-watcher pod log
#   - failed_deployment metric != 0
#   - argocd_unavailable != 0
#   - processed_deployments == 0 (no work was actually processed)
#   - in_progress_tasks does not drain back to 0 (leaked/stuck task tracking)
#   - any of the duration histograms recorded 0 observations, i.e. the
#     refresh / git-writeback / lock-wait / deployment-duration code path never
#     ran (silent regression). With BATCH_MODE set, the per-app writeback/lock-wait
#     gates are replaced by a gitops_batch_size gate (that path records batch size
#     instead, and must show mean batch size > 1 — real coalescing under contention)
#   - a lost update: a fixture app's committed image tag != the last tag the
#     driver deployed to it (per the driver summary JSON)
#   - any failed task in the driver summary
#
# Usage: collect.sh <driver-summary.json>
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

SUMMARY="${1:?usage: collect.sh <driver-summary.json>}"

echo "=== race detector ==="
races=0
for p in $(kubectl -n "$NS_AW" get pods -o name); do
  c=$(kubectl -n "$NS_AW" logs "$p" 2>/dev/null | grep -c 'DATA RACE')
  echo "  $p: $c"
  races=$((races + c))
done
[[ "$races" -eq 0 ]] || bad "$races DATA RACE line(s)"

echo "=== metrics ==="
metrics="$(curl -s -m 10 "${AW_URL}/metrics")"
fd=$(metric_sum failed_deployment "$metrics")
pd=$(metric_sum processed_deployments "$metrics")
# Histogram observation counts. The soak drives many authenticated deploys, each of
# which refreshes ArgoCD (ARGO_REFRESH_APP defaults true) and takes the per-repo
# write-back lock — so all three counts MUST be > 0. A zero means the instrumented
# code path never ran, i.e. that feature silently regressed even if no task failed.
rc=$(metric_sum argocd_refresh_duration_seconds_count "$metrics")
wc=$(metric_sum gitops_writeback_duration_seconds_count "$metrics")
lc=$(metric_sum gitops_lock_wait_duration_seconds_count "$metrics")
# Every successful deployment observes its end-to-end duration, so on a green soak
# (all tasks deployed) this MUST be > 0; a zero means the deployment-duration timing
# never ran.
dc=$(metric_sum deployment_duration_seconds_count "$metrics")
# Batch write-back (GIT_BATCH_WRITEBACK) routes through the coalescing batcher,
# which records gitops_batch_size INSTEAD of the per-app writeback/lock-wait
# histograms. Collected here so the BATCH_MODE gate below can assert on it.
bc=$(metric_sum gitops_batch_size_count "$metrics")
bs=$(metric_sum gitops_batch_size_sum "$metrics")

# These two gauges go through metric_raw, not metric_sum: an ABSENT metric must trip
# their gates rather than read as 0 (see metric_raw in lib.sh).
au=$(metric_raw argocd_unavailable "$metrics")
# in_progress_tasks is decremented in a deferred EndTracking that runs AFTER the
# task's terminal status is written, so the gauge can briefly lag the driver's exit.
# Poll it down to 0 rather than gating on a single scrape (avoids a false FAIL on the
# last task's decrement window); a stuck-non-zero value is a real drain/leak bug.
ip=""
drained() {
  ip=$(metric_raw in_progress_tasks)
  [[ "${ip:-1}" == "0" ]]
}
retry 15 1 drained

echo "  failed_deployment=${fd} processed_deployments=${pd} argocd_unavailable=${au:-?} in_progress_tasks=${ip:-?}"
echo "  refresh_count=${rc} writeback_count=${wc} lock_wait_count=${lc} deployment_count=${dc}"
[[ "${fd:-0}" == "0" ]]  || bad "failed_deployment=${fd}"
[[ "${au:-0}" == "0" ]]  || bad "argocd_unavailable=${au}"
[[ "${pd:-0}" -gt 0 ]]   || bad "processed_deployments=${pd} (expected > 0)"
[[ "${ip:-1}" == "0" ]]  || bad "in_progress_tasks=${ip:-<absent>} did not drain to 0"
[[ "${rc:-0}" -gt 0 ]]   || bad "argocd_refresh_duration_seconds_count=${rc} (expected > 0)"
[[ "${dc:-0}" -gt 0 ]]   || bad "deployment_duration_seconds_count=${dc} (expected > 0)"
if [[ -n "${BATCH_MODE:-}" ]]; then
  # Batch write-back records gitops_batch_size instead of the per-app
  # writeback/lock-wait histograms (those stay 0 by design in this mode).
  echo "  batch_size_count=${bc} batch_size_sum=${bs}"
  [[ "${bc:-0}" -gt 0 ]] || bad "gitops_batch_size_count=${bc} (batch write-back path never ran)"
  # sum > count => at least one flush coalesced more than one app (mean batch
  # size > 1), proving batching actually collapsed concurrent write-backs under
  # contention rather than degenerating into one-app flushes.
  awk "BEGIN{exit !(${bs:-0} > ${bc:-0})}" \
    || bad "gitops_batch_size_sum=${bs} not > _count=${bc} (no coalescing observed)"
else
  [[ "${wc:-0}" -gt 0 ]] || bad "gitops_writeback_duration_seconds_count=${wc} (expected > 0)"
  [[ "${lc:-0}" -gt 0 ]] || bad "gitops_lock_wait_duration_seconds_count=${lc} (expected > 0)"
fi

echo "=== no lost updates ==="
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
gitops_clone "${work}/r"
for app in $(jq -r '.last_tag | keys[]' "$SUMMARY"); do
  want=$(jq -r ".last_tag[\"${app}\"]" "$SUMMARY")
  got=$(override_param "${work}/r/chart/.argocd-source-${app}.yaml" app.image.tag 2>/dev/null)
  if [[ "$got" == "$want" ]]; then
    echo "  ${app}: OK (${got})"
  else
    bad "${app}: LOST UPDATE want=${want} got=${got:-<none>}"
  fi
done

echo "=== task tallies ==="
jq '{submitted,deployed,failed,other}' "$SUMMARY"
tf=$(jq -r '.failed' "$SUMMARY")
[[ "${tf}" == "0" ]] || bad "${tf} failed task(s)"

phase_end COLLECT
