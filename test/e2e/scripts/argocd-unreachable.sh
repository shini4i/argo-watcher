#!/usr/bin/env bash
# Assert the "ArgoCD unreachable" visibility feature end to end (issue #498)
# against the real server, by inducing a genuine ArgoCD outage.
#
# The signal argo-watcher exposes for the frontend banner is the cached ArgoCD
# reachability, refreshed by the 30s liveness probe. This phase drives it through
# a full outage-and-recovery cycle and asserts every observable surface:
#
#   1. Baseline: GET /api/v1/argocd-status reports true (ArgoCD reachable).
#   2. Hold a WebSocket client open (tools/wsprobe) BEFORE the outage so it can
#      capture the transition broadcasts.
#   3. Induce the outage by scaling argocd-server to 0 replicas.
#   4. Within a few probe cycles: GET /api/v1/argocd-status flips to false, and
#      the watcher broadcasts "argocd_down" to the WS client.
#   5. POST /api/v1/tasks now fails fast with 503 {"status":"down"} off the cached
#      state — it does NOT hang on the ArgoCD API retry budget (default
#      ARGO_API_TIMEOUT=60 x ARGO_API_RETRIES=3 ~= 180s), which is the regression
#      guard for the point-3 fast-fail; we assert the response returns well under
#      that budget.
#   6. Recover by scaling argocd-server back to 1: GET /api/v1/argocd-status
#      returns to true, the watcher broadcasts "argocd_up", and POST /tasks is
#      accepted again (202).
#
# Self-contained: the cleanup trap always restores argocd-server to 1 replica, so
# a failed run never leaves ArgoCD down for the phases that follow (or for manual
# debugging). Uses a tokenless POST (202, write-back skipped) so the recovery
# check has no lasting side effect beyond a short-lived rollout monitor.
set -euo pipefail

NS_AW="${NS_AW:-argo-watcher}"
NS_ARGOCD="${NS_ARGOCD:-argocd}"
PORT="${PORT:-18094}"
# Fast-fail budget guard: the cached path does zero network I/O, so a POST during
# the outage returns in well under a second; anything under this bound proves we
# did not fall back to a live ArgoCD check + retry budget.
MAX_FASTFAIL_SECONDS="${MAX_FASTFAIL_SECONDS:-10}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
probe_out="$(mktemp)"
pf_pid=""
cleanup() {
  kill $(jobs -p) 2>/dev/null || true
  # Safety net: always bring ArgoCD back, even on an early failure exit.
  kubectl -n "$NS_ARGOCD" scale deploy/argocd-server --replicas=1 >/dev/null 2>&1 || true
  rm -rf "$bin_dir" "$probe_out"
}
trap cleanup EXIT
( cd "$root" && go build -o "${bin_dir}/wsprobe" ./test/e2e/tools/wsprobe )

start_pf() {
  [[ -n "$pf_pid" ]] && kill "$pf_pid" 2>/dev/null || true
  kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
  pf_pid=$!
  for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done
}

base="http://localhost:${PORT}/api/v1"
task_json='{"app":"app1","author":"e2e","project":"lab","images":[{"image":"traefik/whoami","tag":"v1.10.2"}]}'
# post_task -> sets CODE, BODY and TIME for a tokenless POST /api/v1/tasks.
post_task() {
  local out
  out=$(curl -s -m 30 -w $'\n%{http_code}\n%{time_total}' -X POST -H 'Content-Type: application/json' \
    -d "$task_json" "${base}/tasks")
  TIME="${out##*$'\n'}"; out="${out%$'\n'*}"
  CODE="${out##*$'\n'}"; BODY="${out%$'\n'*}"
}

# status_is <true|false> -> succeeds when GET /argocd-status equals the argument.
status_is() {
  local want="$1"
  curl -s -m 10 "${base}/argocd-status" | jq -e ". == ${want}" >/dev/null 2>&1
}
# wait_status <true|false> <attempts> -> polls status_is on a 5s tick.
wait_status() {
  local want="$1" attempts="$2"
  for _ in $(seq 1 "$attempts"); do status_is "$want" && return 0; sleep 5; done
  return 1
}
# wait_ws <message> -> waits up to ~30s for wsprobe to capture `MSG <message>`.
wait_ws() {
  local message="$1"
  for _ in $(seq 1 6); do grep -q "^MSG ${message}\$" "$probe_out" && return 0; sleep 5; done
  return 1
}

fail=0
start_pf

# --- 1. Baseline ---------------------------------------------------------------
if status_is true; then
  echo "  OK   GET /argocd-status -> true (ArgoCD reachable at baseline)"
else
  echo "  FAIL GET /argocd-status not true at baseline"; fail=1
fi

# --- 2. Hold a WS client open before the outage --------------------------------
# DURATION comfortably exceeds the whole outage+recovery walk (each reachability
# poll below is bounded to ~200s) so the probe never hits its own deadline before
# we have observed both transitions.
WS_URL="ws://localhost:${PORT}/ws" DURATION=900s "${bin_dir}/wsprobe" >"$probe_out" 2>/dev/null &
ws_open=0
for _ in $(seq 1 20); do grep -q '^OPEN$' "$probe_out" && { ws_open=1; break; }; sleep 1; done
if [[ "$ws_open" != "1" ]]; then
  echo "  FAIL WS probe never connected before the outage"; fail=1
fi

# --- 3. Induce the outage ------------------------------------------------------
echo "  ...  scaling argocd-server to 0 replicas to sever ArgoCD connectivity"
kubectl -n "$NS_ARGOCD" scale deploy/argocd-server --replicas=0 >/dev/null

# --- 4. Reachability flips to false + argocd_down broadcast --------------------
# Up to ~200s: the liveness probe runs every 30s and the watcher samples every 5s;
# the wide margin absorbs a probe mid-cycle when the outage begins.
if wait_status false 40; then
  echo "  OK   GET /argocd-status -> false after the outage"
else
  echo "  FAIL GET /argocd-status never flipped to false during the outage"; fail=1
fi
if [[ "$ws_open" == "1" ]]; then
  if wait_ws argocd_down; then
    echo "  OK   WS client received the 'argocd_down' broadcast"
  else
    echo "  FAIL no 'argocd_down' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"; fail=1
  fi
fi

# --- 5. POST fails fast off the cached state -----------------------------------
post_task
if [[ "$CODE" == "503" ]] && echo "$BODY" | jq -e '.status == "down"' >/dev/null 2>&1; then
  echo "  OK   POST /tasks -> 503 {\"status\":\"down\"} during the outage"
else
  echo "  FAIL POST /tasks during outage: code=${CODE} body=${BODY} (want 503 down)"; fail=1
fi
if awk -v t="$TIME" -v m="$MAX_FASTFAIL_SECONDS" 'BEGIN{exit !(t+0 < m+0)}'; then
  echo "  OK   POST /tasks returned in ${TIME}s (< ${MAX_FASTFAIL_SECONDS}s: cached fast-fail, no retry budget)"
else
  echo "  FAIL POST /tasks took ${TIME}s (>= ${MAX_FASTFAIL_SECONDS}s: looks like a live ArgoCD check + retry)"; fail=1
fi

# --- 6. Recover ----------------------------------------------------------------
echo "  ...  scaling argocd-server back to 1 replica"
kubectl -n "$NS_ARGOCD" scale deploy/argocd-server --replicas=1 >/dev/null
kubectl -n "$NS_ARGOCD" rollout status deploy/argocd-server --timeout=180s >/dev/null

if wait_status true 40; then
  echo "  OK   GET /argocd-status -> true after recovery"
else
  echo "  FAIL GET /argocd-status never returned to true after recovery"; fail=1
fi
if [[ "$ws_open" == "1" ]]; then
  if wait_ws argocd_up; then
    echo "  OK   WS client received the 'argocd_up' broadcast"
  else
    echo "  FAIL no 'argocd_up' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"; fail=1
  fi
fi
post_task
if [[ "$CODE" == "202" ]]; then
  echo "  OK   POST /tasks -> 202 accepted (deploys resumed)"
else
  echo "  FAIL POST /tasks after recovery: code=${CODE} body=${BODY} (want 202)"; fail=1
fi

if [[ "$fail" -eq 0 ]]; then echo "ARGOCD-UNREACHABLE: PASS"; else echo "ARGOCD-UNREACHABLE: FAIL"; exit 1; fi
