#!/usr/bin/env bash
# Assert argo-watcher shuts down gracefully and drains in-flight WebSocket
# connections, guarding the hijack/shutdown-ordering races fixed upstream
# (connWg not tracking the in-flight handshake; connWg.Wait racing new
# handshakes). Runs against the -race build, so a regression in the shutdown
# path surfaces as a logged DATA RACE or a WaitGroup panic.
#
#   1. Hold N WebSocket clients open (tools/wsprobe) against the live server.
#   2. Follow the pod's logs, then delete the pod with a generous grace period so
#      the container runs its full graceful shutdown (SIGTERM -> srv.Shutdown ->
#      env.Shutdown drains the hijacked WS goroutines).
#   3. Assert every WS client saw a graceful close: code 1001 (GoingAway) with
#      reason "server shutdown" — the exact frame checkConnection sends on drain,
#      NOT an abrupt socket drop.
#   4. Assert the captured shutdown logs show the ordered graceful path
#      ("shutting down server..." -> WS drain -> "server exited") with NO data
#      race, NO panic, and NO drain-timeout warning.
#   5. Assert the recreated pod comes back Ready and reaches real Argo.
#
# Scope note: in-flight *deploys* are deliberately not asserted here. A deploy's
# write-back + Argo sync outlives the 30s HTTP drain budget by design and is
# resumed by the poller, not the HTTP request — asserting it would test the
# poller, not shutdown. This phase asserts the shutdown/drain contract only.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

WS_CLIENTS="${WS_CLIENTS:-3}"
STS="${STS:-argo-watcher}"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
log_out="${work}/pod.log"
log_pid=""
cleanup() {
  # jobs -p prints one PID per line; splitting them into separate arguments is the
  # point, and they are always bare digits.
  # shellcheck disable=SC2046
  kill $(jobs -p) 2>/dev/null || true
  rm -rf "$bin_dir" "$work"
}
trap cleanup EXIT
build_bin "$bin_dir" ./test/e2e/tools/wsprobe || die "wsprobe build failed"
wsprobe="$BIN"

pod="${STS}-0"
old_uid=$(kubectl -n "$NS_AW" get pod "$pod" -o jsonpath='{.metadata.uid}')

wait_service || die "argo-watcher not reachable on ${AW_URL}"

# --- 1. Hold WebSocket clients open through the shutdown -----------------------
echo "opening ${WS_CLIENTS} WebSocket client(s)"
probe_pids=()
for i in $(seq 1 "$WS_CLIENTS"); do
  WS_URL="$AW_WS_URL" DURATION=120s "$wsprobe" >"${work}/probe${i}.out" 2>/dev/null &
  probe_pids+=($!)
done
# Wait until every probe reports OPEN before triggering shutdown, so the
# connections are provably registered in the server's set — a fixed sleep raced
# the handshake on a loaded host and left the drain with nothing to observe.
for i in $(seq 1 "$WS_CLIENTS"); do
  wait_ws_open "${work}/probe${i}.out" \
    || die "client ${i} never established its WebSocket connection"
done

# --- 2. Capture logs, then trigger a graceful pod termination ------------------
# Follow the pod's logs to a file BEFORE deleting: once the pod is gone its logs
# go with it (a recreated StatefulSet pod is a new object, so `logs --previous`
# would not have them). `logs -f` exits when the container stops.
kubectl -n "$NS_AW" logs -f "$pod" >"$log_out" 2>/dev/null &
log_pid=$!
sleep 2
echo "deleting pod ${pod} (grace 60s) to trigger graceful shutdown"
# --grace-period=60 overrides the spec so the drain (srv.Shutdown 30s + WS drain
# 10s) completes before SIGKILL; --wait=false returns so we can watch the clients.
kubectl -n "$NS_AW" delete pod "$pod" --grace-period=60 --wait=false >/dev/null

# --- 3. Every WS client must observe the graceful GoingAway close --------------
gone() { local pid="$1"; ! kill -0 "$pid" 2>/dev/null; }
for i in $(seq 1 "$WS_CLIENTS"); do
  # wsprobe exits when its connection closes; bound the wait so a hung client fails.
  retry 30 1 gone "${probe_pids[$((i - 1))]}" || true
  out="${work}/probe${i}.out"
  if grep -q '^CLOSED code=1001 reason=server shutdown$' "$out"; then
    ok "client ${i} drained gracefully (1001 server shutdown)"
  else
    bad "client ${i} did not see a graceful close: $(tr '\n' ',' <"$out")"
  fi
done

# --- 4. The captured shutdown logs must show the clean ordered path ------------
retry 30 1 gone "$log_pid" || true
kill "$log_pid" 2>/dev/null || true
assert_log() {  # pattern human-label want(present|absent)
  local pattern="$1" label="$2" want="$3" found
  if grep -qF "$pattern" "$log_out"; then found=1; else found=0; fi
  if { [[ "$want" == present && "$found" == 1 ]] || [[ "$want" == absent && "$found" == 0 ]]; }; then
    ok "log ${want}: ${label}"
  else
    bad "log ${want} expected but not: ${label}"
  fi
}
assert_log "shutting down server..."                   "shutdown initiated"        present
# This one line is Debug-level (env.go); it is asserted because the lab runs at
# logLevel: debug (values/argo-watcher.yaml). If that is ever lowered to info this
# check fails misleadingly — the drain itself would still be fine.
assert_log "All WebSocket connections closed gracefully" "WS goroutines drained"   present
assert_log "server exited"                             "run loop returned"         present
assert_log "Shutdown timeout reached"                  "no WS drain timeout"       absent
assert_log "DATA RACE"                                 "race detector clean"       absent
assert_log "panic:"                                    "no panic during shutdown"  absent

# --- 5. The recreated pod must come back Ready and reach Argo ------------------
echo "waiting for ${pod} to be recreated and Ready"
recreated_ready() {
  local uid rdy
  uid=$(kubectl -n "$NS_AW" get pod "$pod" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)
  rdy=$(kubectl -n "$NS_AW" get pod "$pod" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
  [[ -n "$uid" && "$uid" != "$old_uid" && "$rdy" == "True" ]]
}
if ! retry 60 3 recreated_ready; then
  bad "${pod} did not come back Ready after shutdown"
else
  # No port-forward to re-establish: the NodePort URL survived the pod swap.
  wait_service || die "argo-watcher never answered after the pod came back"
  code=$(curl -s -m 10 -o /dev/null -w '%{http_code}' "${AW_URL}/healthz")
  # metric_raw, not metric_sum: an ABSENT gauge must fail this gate, not read as 0.
  unavail=$(metric_raw argocd_unavailable)
  if [[ "$code" == "200" && "$unavail" == "0" ]]; then
    ok "recreated pod healthy and reaching Argo (healthz=200 argocd_unavailable=0)"
  else
    bad "recreated pod not healthy: healthz=${code} argocd_unavailable=${unavail:-<absent>}"
  fi
fi

phase_end SHUTDOWN-DRAIN
