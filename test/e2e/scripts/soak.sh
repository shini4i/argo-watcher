#!/usr/bin/env bash
# Git-conflict soak: a competitor writer plus concurrent deploys against the shared
# gitops repo, then the collect.sh gates. This is the phase that reproduces the
# real-world pain — many apps' write-backs contending on one repo — so its gates are
# strict (0 failed tasks, 0 lost updates).
#
# Usage: [APPS=..] [WORKERS=..] [WS_CLIENTS=..] [SOAK=..] [SOAK_SECONDS=..] soak.sh
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APPS="${APPS:-5}"
WORKERS="${WORKERS:-10}"
WS_CLIENTS="${WS_CLIENTS:-10}"
SOAK="${SOAK:-5m}"
SOAK_SECONDS="${SOAK_SECONDS:-300}"
COMPETITOR_INTERVAL="${COMPETITOR_INTERVAL:-2}"

# Wait for all fixture apps to be Healthy so the driver never races a cold app,
# which would surface as a spurious "app not found".
for i in $(seq 1 "$APPS"); do
  wait_app "app$i" Healthy 40 || die "app$i never became Healthy (last: ${APP_STATE:-unknown})"
done
wait_service || die "argo-watcher not reachable on ${AW_URL}"

summary="$(mktemp)"
comp=""
# The competitor keeps pushing to the shared gitops repo for up to SOAK_SECONDS, so an
# early exit (a failed wait, a driver crash, Ctrl-C) must not leave it running.
trap 'rm -f "$summary"; [[ -n "$comp" ]] && kill "$comp" 2>/dev/null || true' EXIT
SECONDS_TOTAL="$SOAK_SECONDS" INTERVAL="$COMPETITOR_INTERVAL" "${here}/competitor.sh" & comp=$!

echo "=== soak: ${WORKERS} workers x ${APPS} apps for ${SOAK}, competitor@${COMPETITOR_INTERVAL}s ==="
(
  cd "$E2E_DIR" || exit 1
  APPS="$APPS" WORKERS="$WORKERS" WS_CLIENTS="$WS_CLIENTS" DURATION="$SOAK" \
    DEPLOY_TOKEN="$DEPLOY_TOKEN" BASE_URL="$AW_URL" WS_URL="$AW_WS_URL" \
    go run ./load >"$summary"
)
drv=$?
cat "$summary"
wait $comp 2>/dev/null || true

"${here}/collect.sh" "$summary"
col=$?

[[ "$drv" -eq 0 && "$col" -eq 0 ]] || die "soak failed (driver=${drv} collect=${col})"
echo "SOAK: PASS"
