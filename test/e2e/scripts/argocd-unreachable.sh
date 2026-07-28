#!/usr/bin/env bash
# Assert the "ArgoCD unreachable" visibility feature end to end (issue #498)
# against the real server, by inducing a genuine ArgoCD outage.
#
# The signal argo-watcher exposes for the frontend banner is the cached ArgoCD
# reachability, refreshed by the 30s liveness probe. This phase drives it through
# a full outage-and-recovery cycle and asserts every observable surface:
#
#   1. Baseline: GET /api/v1/reachability reports true (ArgoCD reachable).
#   2. Hold a WebSocket client open (tools/wsprobe) BEFORE the outage so it can
#      capture the transition broadcasts.
#   3. Induce the outage by scaling argocd-server to 0 replicas.
#   4. Within a few probe cycles: GET /api/v1/reachability flips to
#      {"available":false,"reason":"argocd"} (only ArgoCD was scaled down, so the
#      cause is ArgoCD, not the state backend), and the watcher broadcasts
#      "argocd_down:argocd" to the WS client.
#   5. POST /api/v1/tasks now fails fast with 503 {"status":"down"} off the cached
#      state — it does NOT hang on the ArgoCD API retry budget (default
#      ARGO_API_TIMEOUT=60 x ARGO_API_RETRIES=3 ~= 180s), which is the regression
#      guard for the point-3 fast-fail; we assert the response returns well under
#      that budget.
#   6. Recover by scaling argocd-server back to 1: GET /api/v1/reachability
#      returns to true, the watcher broadcasts "argocd_up", and POST /tasks is
#      accepted again (202).
#
# Self-contained: the cleanup trap always restores argocd-server to 1 replica, so
# a failed run never leaves ArgoCD down for the phases that follow (or for manual
# debugging). Uses a tokenless POST (202, write-back skipped) so the recovery
# check has no lasting side effect beyond a short-lived rollout monitor.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

# Fast-fail budget guard: the cached path does zero network I/O, so a POST during
# the outage returns in well under a second; anything under this bound proves we
# did not fall back to a live ArgoCD check + retry budget.
MAX_FASTFAIL_SECONDS="${MAX_FASTFAIL_SECONDS:-10}"
# The POST during the outage must be allowed to run long enough to PROVE it is not
# waiting out the ArgoCD retry budget, so this timeout sits above the guard.
REQ_TIMEOUT=30

bin_dir="$(mktemp -d)"
probe_out="$(mktemp)"
cleanup() {
  # jobs -p prints one PID per line; splitting them into separate arguments is the
  # point, and they are always bare digits.
  # shellcheck disable=SC2046
  kill $(jobs -p) 2>/dev/null || true
  # Safety net: always bring ArgoCD back, even on an early failure exit.
  kubectl -n "$NS_ARGOCD" scale deploy/argocd-server --replicas=1 >/dev/null 2>&1 || true
  rm -rf "$bin_dir" "$probe_out"
}
trap cleanup EXIT
build_bin "$bin_dir" ./test/e2e/tools/wsprobe || die "wsprobe build failed"
wsprobe="$BIN"

TASK="$(task_json app1 v1.10.2)"

# status_is <true|false> -> succeeds when GET /reachability .available equals it.
status_is() {
  curl -s -m 10 "${AW_API}/reachability" | jq -e ".available == $1" >/dev/null 2>&1
}
# reason_is <reason> -> succeeds when GET /reachability .reason equals the arg.
reason_is() {
  curl -s -m 10 "${AW_API}/reachability" | jq -e ".reason == \"$1\"" >/dev/null 2>&1
}

wait_service || die "argo-watcher not reachable on ${AW_URL}"

# --- 1. Baseline ---------------------------------------------------------------
if status_is true; then
  ok "GET /reachability -> true (ArgoCD reachable at baseline)"
else
  bad "GET /reachability not true at baseline"
fi

# --- 2. Hold a WS client open before the outage --------------------------------
# DURATION comfortably exceeds the whole outage+recovery walk (each reachability
# poll below is bounded to ~200s) so the probe never hits its own deadline before
# we have observed both transitions.
WS_URL="$AW_WS_URL" DURATION=900s "$wsprobe" >"$probe_out" 2>/dev/null &
ws_open=1
if ! wait_ws_open "$probe_out"; then
  bad "WS probe never connected before the outage"
  ws_open=0
fi

# --- 3. Induce the outage ------------------------------------------------------
echo "  ...  scaling argocd-server to 0 replicas to sever ArgoCD connectivity"
kubectl -n "$NS_ARGOCD" scale deploy/argocd-server --replicas=0 >/dev/null

# --- 4. Reachability flips to false + argocd_down broadcast --------------------
# Up to ~200s: the liveness probe runs every 30s and the watcher samples every 5s;
# the wide margin absorbs a probe mid-cycle when the outage begins.
if retry 40 5 status_is false; then
  ok "GET /reachability -> false after the outage"
else
  bad "GET /reachability never flipped to false during the outage"
fi
# The state backend stays up (only argocd-server was scaled down), so the cause
# must be identified as "argocd" — not the combined "both" or a bare outage.
if reason_is argocd; then
  ok "GET /reachability -> reason \"argocd\" (ArgoCD-only outage)"
else
  bad "GET /reachability reason not \"argocd\" during the outage"
fi
if [[ "$ws_open" == "1" ]]; then
  if wait_ws "$probe_out" argocd_down:argocd; then
    ok "WS client received the 'argocd_down:argocd' broadcast"
  else
    bad "no 'argocd_down:argocd' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"
  fi
fi

# --- 5. POST fails fast off the cached state -----------------------------------
post_task "$TASK"
if [[ "$CODE" == "503" ]] && jq -e '.status == "down"' <<<"$BODY" >/dev/null 2>&1; then
  ok "POST /tasks -> 503 {\"status\":\"down\"} during the outage"
else
  bad "POST /tasks during outage: code=${CODE} body=${BODY} (want 503 down)"
fi
if awk -v t="$TIME" -v m="$MAX_FASTFAIL_SECONDS" 'BEGIN{exit !(t+0 < m+0)}'; then
  ok "POST /tasks returned in ${TIME}s (< ${MAX_FASTFAIL_SECONDS}s: cached fast-fail, no retry budget)"
else
  bad "POST /tasks took ${TIME}s (>= ${MAX_FASTFAIL_SECONDS}s: looks like a live ArgoCD check + retry)"
fi

# --- 6. Recover ----------------------------------------------------------------
echo "  ...  scaling argocd-server back to 1 replica"
kubectl -n "$NS_ARGOCD" scale deploy/argocd-server --replicas=1 >/dev/null
kubectl -n "$NS_ARGOCD" rollout status deploy/argocd-server --timeout=180s >/dev/null

if retry 40 5 status_is true; then
  ok "GET /reachability -> true after recovery"
else
  bad "GET /reachability never returned to true after recovery"
fi
if [[ "$ws_open" == "1" ]]; then
  if wait_ws "$probe_out" argocd_up; then
    ok "WS client received the 'argocd_up' broadcast"
  else
    bad "no 'argocd_up' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"
  fi
fi
post_task "$TASK"
if [[ "$CODE" == "202" ]]; then
  ok "POST /tasks -> 202 accepted (deploys resumed)"
else
  bad "POST /tasks after recovery: code=${CODE} body=${BODY} (want 202)"
fi

phase_end ARGOCD-UNREACHABLE
