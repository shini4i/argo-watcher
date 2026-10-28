#!/usr/bin/env bash
# Prove DEGRADED_GRACE_PERIOD rides out a transient degradation (#631): app4 is Degraded while
# the deploy rolls out, recovers inside the window, and the deploy must still report deployed.
# Usage: DEPLOY_TOKEN=... degraded-grace.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="app4"
GRACE_LOG="Application is degraded, waiting for the grace period"

bin_dir="$(mktemp -d)"
out="${bin_dir}/client.out"
fixture_applied=0
cleanup() {
  if [[ "$fixture_applied" == 1 ]]; then
    "${here}/failure-fixture.sh" remove || true
    refresh_all_apps
  fi
  rm -rf "$bin_dir"
  return
}
trap cleanup EXIT

build_client "$bin_dir" || die "client build failed"
require_app_synced "$APP" 60
wait_service || die "argo-watcher never answered on ${AW_URL}"

tag=$(other_tag "$APP")
since=$(date -u +%Y-%m-%dT%H:%M:%SZ)

fixture_applied=1
"${here}/failure-fixture.sh" degraded
refresh_all_apps
wait_app "$APP" "Degraded" 40 || die "${APP} never became Degraded (last: ${APP_STATE:-unknown})"

echo "deploying ${APP} -> ${IMAGE}:${tag} while it is Degraded"
run_client "$APP" "$tag" ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" TASK_TIMEOUT="300" >"$out" 2>&1 &
client_pid=$!

# The fixture comes off only once argo-watcher has seen the new image Degraded.
grace_logged() {
  local pod
  for pod in $(kubectl -n "$NS_AW" get pods -o name); do
    kubectl -n "$NS_AW" logs "$pod" --since-time="$since" 2>/dev/null | grep -qF "$GRACE_LOG" && return 0
  done
  return 1
}
retry 24 5 grace_logged || die "argo-watcher never logged «${GRACE_LOG}»"
ok "argo-watcher is waiting out the grace period"

"${here}/failure-fixture.sh" remove
fixture_applied=0
refresh_all_apps

rc=0
wait "$client_pid" || rc=$?
# shellcheck disable=SC2001  # per-line prefix needs a regex anchor; ${//} can't do it
echo "  client output: $(sed 's/^/    | /' "$out")"

wait_app "$APP" "Synced/Healthy" 60 || note "${APP} did not return to Synced/Healthy (last: ${APP_STATE:-unknown})"

if [[ "$rc" -eq 0 ]]; then
  echo "DEGRADED-GRACE: PASS (a transient degradation did not fail the deploy)"
  exit 0
fi
echo "DEGRADED-GRACE: FAIL (client exited ${rc}: the transient degradation failed the deploy)"
exit 1
