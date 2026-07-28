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
#   COMPETITOR_INTERVAL.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

# Required by helm_apply_aw; asserted up front rather than mid-soak.
: "${AW_CHART_REPO:?AW_CHART_REPO is required}" "${AW_CHART_VERSION:?AW_CHART_VERSION is required}"
# Not configurable: extra_envs_index must count entries in the SAME file
# helm_apply_aw applies, or the appended --set would overwrite a real entry.
VALUES="${E2E_DIR}/values/argo-watcher.yaml"
APPS="${APPS:-5}"
WORKERS="${WORKERS:-10}"
WS_CLIENTS="${WS_CLIENTS:-10}"
SOAK="${SOAK:-2m}"
SOAK_SECONDS="${SOAK_SECONDS:-120}"
COMPETITOR_INTERVAL="${COMPETITOR_INTERVAL:-2}"

cleanup() {
  # jobs -p prints one PID per line; splitting them into separate arguments is the
  # point, and they are always bare digits.
  # shellcheck disable=SC2046
  kill $(jobs -p) 2>/dev/null || true
  return
}
trap cleanup EXIT

# Append GIT_BATCH_WRITEBACK as the next free extraEnvs entry (see extra_envs_index
# in lib.sh); helm_apply_aw handles the --reset-values determinism.
idx=$(extra_envs_index "$VALUES")

echo "=== enabling GIT_BATCH_WRITEBACK on the live release (extraEnvs[${idx}]) ==="
helm_apply_aw --set-string "extraEnvs[${idx}].name=GIT_BATCH_WRITEBACK" \
              --set-string "extraEnvs[${idx}].value=true"
wait_service || die "argo-watcher never came back after enabling batch write-back"

echo "=== waiting for the ${APPS} fixture apps to be Healthy ==="
for i in $(seq 1 "$APPS"); do
  wait_app "app$i" Healthy 40 || die "app$i never became Healthy (last: ${APP_STATE:-unknown})"
done

echo "=== batch soak: ${WORKERS} workers x ${APPS} apps for ${SOAK}, competitor@${COMPETITOR_INTERVAL}s ==="
summary="$(mktemp)"
(
  cd "$E2E_DIR" || exit 1
  SECONDS_TOTAL="$SOAK_SECONDS" INTERVAL="$COMPETITOR_INTERVAL" ./scripts/competitor.sh & comp=$!
  APPS="$APPS" WORKERS="$WORKERS" WS_CLIENTS="$WS_CLIENTS" DURATION="$SOAK" \
    DEPLOY_TOKEN="$DEPLOY_TOKEN" BASE_URL="$AW_URL" WS_URL="$AW_WS_URL" \
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
helm_apply_aw
wait_service || die "argo-watcher never came back after reverting batch write-back"

if [[ "$drv" -eq 0 && "$col" -eq 0 ]]; then
  echo "BATCH-WRITEBACK: PASS"
else
  echo "BATCH-WRITEBACK: FAIL (driver=$drv collect=$col)"
  exit 1
fi
