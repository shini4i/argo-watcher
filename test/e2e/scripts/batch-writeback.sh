#!/usr/bin/env bash
# Prove opt-in batch write-back (GIT_BATCH_WRITEBACK) under REAL push contention.
#
# The base lab runs the serialized per-app write-back path; the `load` phase soaks
# it. This phase flips the SAME release to GIT_BATCH_WRITEBACK=true and re-runs the
# contention soak (10 workers x 5 apps sharing one gitops repo + a competitor writer
# forcing retries), then asserts:
#
#   1. correctness is unchanged under batching — the reused collect.sh gates fire:
#      zero failed tasks, zero failed_deployment, and NO lost updates (every app's
#      committed tag equals the last tag deployed). This is the maintainer's top
#      priority: batching must never lose or clobber an update under concurrency.
#   2. batching actually happened AND coalesced — collect.sh (BATCH_MODE) gates on
#      gitops_batch_size_count > 0 and _sum > _count (mean batch size > 1), i.e.
#      concurrent write-backs to one repo were collapsed into shared clone+push
#      flushes rather than degenerating into one-app flushes.
#
# The flag is a server-global env, so — like lockdown.sh / state-postgres.sh — this
# phase toggles it on the live release for its own duration and REVERTS before
# returning, leaving the release on the default serialized path for later phases.
#
# Required env: AW_CHART_REPO, AW_CHART_VERSION (to helm-upgrade the release).
# Optional env: DEPLOY_TOKEN, APPS, WORKERS, WS_CLIENTS, SOAK, SOAK_SECONDS,
#   COMPETITOR_INTERVAL, PORT, VALUES.
set -uo pipefail

NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18094}"
VALUES="${VALUES:-values/argo-watcher.yaml}"
AW_CHART_REPO="${AW_CHART_REPO:?AW_CHART_REPO required (chart repo URL)}"
AW_CHART_VERSION="${AW_CHART_VERSION:?AW_CHART_VERSION required (pinned chart version)}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
APPS="${APPS:-5}"
WORKERS="${WORKERS:-10}"
WS_CLIENTS="${WS_CLIENTS:-10}"
SOAK="${SOAK:-2m}"
SOAK_SECONDS="${SOAK_SECONDS:-120}"
COMPETITOR_INTERVAL="${COMPETITOR_INTERVAL:-2}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
e2e_dir="$(cd "${here}/.." && pwd)"

pf_pid=""
cleanup() { kill $(jobs -p) 2>/dev/null || true; return; }
trap cleanup EXIT

# helm_apply reconfigures the live release from the values file + these --set args
# alone. --reset-values makes each apply deterministic: without it a prior
# `--set extraEnvs[N]` would carry forward, so the revert (values-file only) would
# NOT drop the injected GIT_BATCH_WRITEBACK. Same mechanism as lockdown.sh.
helm_apply() {
  helm upgrade --install argo-watcher argo-watcher --repo "$AW_CHART_REPO" \
    --version "$AW_CHART_VERSION" -n "$NS_AW" -f "${e2e_dir}/${VALUES}" --reset-values \
    --set image.tag=race "$@" >/dev/null
  kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s >/dev/null
  return
}

start_pf() {
  [[ -n "$pf_pid" ]] && kill "$pf_pid" 2>/dev/null || true
  kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
  pf_pid=$!
  for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done
  return
}

# Append GIT_BATCH_WRITEBACK as the next extraEnvs entry. The index is the count of
# entries in the values file's extraEnvs block (read from the file, not the live
# release), scoped to the block so an unrelated same-indented "- name:" cannot shift
# it — identical to lockdown.sh.
idx=$(awk '/^extraEnvs:/{f=1;next} f&&/^[^[:space:]#]/{f=0} f&&/^  - name:/{c++} END{print c+0}' "${e2e_dir}/${VALUES}")

echo "=== enabling GIT_BATCH_WRITEBACK on the live release (extraEnvs[${idx}]) ==="
helm_apply --set-string "extraEnvs[${idx}].name=GIT_BATCH_WRITEBACK" \
           --set-string "extraEnvs[${idx}].value=true"
start_pf

echo "=== waiting for the ${APPS} fixture apps to be Healthy ==="
for i in $(seq 1 "$APPS"); do
  for _ in $(seq 1 40); do
    [[ "$(kubectl -n argocd get application "app$i" -o jsonpath='{.status.health.status}' 2>/dev/null)" == "Healthy" ]] && break
    sleep 3
  done
done

echo "=== batch soak: ${WORKERS} workers x ${APPS} apps for ${SOAK}, competitor@${COMPETITOR_INTERVAL}s ==="
summary="$(mktemp)"
drv=1
(
  cd "$e2e_dir" || exit 1
  SECONDS_TOTAL="$SOAK_SECONDS" INTERVAL="$COMPETITOR_INTERVAL" ./scripts/competitor.sh & comp=$!
  APPS="$APPS" WORKERS="$WORKERS" WS_CLIENTS="$WS_CLIENTS" DURATION="$SOAK" \
    DEPLOY_TOKEN="$DEPLOY_TOKEN" BASE_URL="http://localhost:${PORT}" WS_URL="ws://localhost:${PORT}/ws" \
    go run ./load >"$summary"
  rc=$?
  wait $comp 2>/dev/null || true
  exit $rc
)
drv=$?
cat "$summary"

# Reuse the soak gate. BATCH_MODE swaps the per-app writeback/lock-wait histogram
# gates (0 by design on the batcher path) for the gitops_batch_size gate, while
# keeping the zero-lost-update / zero-failed / race-detector gates intact.
BATCH_MODE=1 "${here}/collect.sh" "$summary"
col=$?

echo "=== reverting GIT_BATCH_WRITEBACK (restore the default serialized path) ==="
helm_apply
start_pf

if [[ "$drv" -eq 0 && "$col" -eq 0 ]]; then
  echo "BATCH-WRITEBACK: PASS"
else
  echo "BATCH-WRITEBACK: FAIL (driver=$drv collect=$col)"
  exit 1
fi
