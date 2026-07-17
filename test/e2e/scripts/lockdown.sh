#!/usr/bin/env bash
# Assert the scheduled-lockdown deploy-freeze end to end against the real server.
#
# LOCKDOWN_SCHEDULE is a server-global config, so it cannot be enabled in the
# shared install without blocking every other deploy phase. This phase therefore
# toggles it on the live release for its own duration and reverts before exiting:
#
#   1. helm-upgrade the release with a schedule whose window OPENS ~3 min in the
#      future, so the pod boots UNLOCKED and then crosses into the lockdown window
#      while we watch — the only way to observe a *scheduled* transition (the
#      watcher notifies on state change, not at boot).
#   2. While unlocked, hold a WebSocket client open (tools/wsprobe).
#   3. When the window opens: GET /deploy-lock flips to `true` (evaluated live),
#      POST /api/v1/tasks is rejected with 406 "lockdown is active", and within
#      one poll interval the lockdown watcher broadcasts "locked" to the WS client.
#   4. Revert the schedule (pod restart) and prove deploys are accepted again.
#
# The 406 + GET-true + revert-accepts checks are deterministic. The WS "locked"
# broadcast is asserted only when we confirmed the pod booted before the window
# (GET was false first): if a slow rollout boots in-window the transition already
# happened un-observed, so that sub-check is skipped with a logged note rather
# than flaking. Manual (Keycloak) lock/unlock WS notifications are a separate,
# deterministic trigger covered by the heavy-tier Keycloak phase.
set -euo pipefail

NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18093}"
# Helm coordinates for the in-place toggle (passed from the Taskfile).
AW_CHART_REPO="${AW_CHART_REPO:?AW_CHART_REPO is required}"
AW_CHART_VERSION="${AW_CHART_VERSION:?AW_CHART_VERSION is required}"
VALUES="${VALUES:-values/argo-watcher.yaml}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
probe_out="$(mktemp)"
pf_pid=""
cleanup() { kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir" "$probe_out"; }
trap cleanup EXIT
( cd "$root" && go build -o "${bin_dir}/wsprobe" ./test/e2e/tools/wsprobe )

# (re)start the port-forward after a rollout swaps the pod out.
start_pf() {
  [ -n "$pf_pid" ] && kill "$pf_pid" 2>/dev/null || true
  kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
  pf_pid=$!
  for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done
}

helm_apply() {  # extra --set args reconfigure the release in place
  # --reset-values makes each apply deterministic from the values file + these
  # --set args alone: without it helm carries a prior `--set extraEnvs[N]` forward,
  # so the revert (values file only) would NOT drop the injected LOCKDOWN_SCHEDULE.
  helm upgrade --install argo-watcher argo-watcher --repo "$AW_CHART_REPO" \
    --version "$AW_CHART_VERSION" -n "$NS_AW" -f "$VALUES" --reset-values \
    --set image.tag=race "$@" >/dev/null
  kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s >/dev/null
}

base="http://localhost:${PORT}/api/v1"
task_json='{"app":"app1","author":"e2e","project":"lab","images":[{"image":"traefik/whoami","tag":"v1.10.2"}]}'
# post_task -> sets CODE and BODY for a tokenless POST /api/v1/tasks.
post_task() {
  local out
  out=$(curl -s -m 10 -w $'\n%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d "$task_json" "${base}/tasks")
  CODE="${out##*$'\n'}"; BODY="${out%$'\n'*}"
}
fail=0

# --- 1. Enable a schedule whose window opens ~3 min out ------------------------
# date arithmetic (GNU date) rolls days/weeks over cleanly, so the window is
# valid whatever the wall-clock day. 12h end keeps it comfortably open for the
# whole phase; the revert (not the end time) is what unlocks.
#   -u  : compute in UTC, because the server evaluates the schedule against
#         time.Now() in the pod's timezone — distroless has no zoneinfo, so that
#         is UTC. A host in another zone would otherwise offset the window.
#   LC_ALL=C : force the English 3-letter weekday (Sun..Sat) dayToWeekday expects;
#         a localized host (e.g. "Pk"/"S ") would produce an unparseable schedule.
start_day=$(LC_ALL=C date -u -d '+180 seconds' +%a); start_hm=$(date -u -d '+180 seconds' +%H:%M)
end_day=$(LC_ALL=C date -u -d '+12 hours' +%a);      end_hm=$(date -u -d '+12 hours' +%H:%M)
schedule="${start_day} ${start_hm} - ${end_day} ${end_hm}"
echo "lockdown window: ${schedule} (opens ~180s from now)"

# Append LOCKDOWN_SCHEDULE as the next extraEnvs entry. The index is the count of
# entries in the values file (each is a top-level "  - name:" under extraEnvs) —
# read from the file, not the live release, so it is independent of release state.
idx=$(grep -c '^  - name:' "$VALUES")
helm_apply --set-string "extraEnvs[${idx}].name=LOCKDOWN_SCHEDULE" \
           --set-string "extraEnvs[${idx}].value=${schedule}"
start_pf

# --- 2. Confirm we booted pre-window, then hold a WS client open ---------------
assert_ws=1
if curl -s -m 10 "${base}/deploy-lock" | jq -e '. == false' >/dev/null 2>&1; then
  echo "  OK   booted unlocked (pre-window); watching for the scheduled 'locked' broadcast"
  WS_URL="ws://localhost:${PORT}/ws" DURATION=400s "${bin_dir}/wsprobe" >"$probe_out" 2>/dev/null &
else
  echo "  NOTE rollout booted in-window; skipping the WS-transition sub-check (not a failure)"
  assert_ws=0
fi

# --- 3. Wait for the window to open, then assert the locked behaviour ----------
locked=0
for _ in $(seq 1 60); do   # up to ~300s: window opens at +180s, then evaluated live
  if curl -s -m 10 "${base}/deploy-lock" | jq -e '. == true' >/dev/null 2>&1; then locked=1; break; fi
  sleep 5
done
if [[ "$locked" == "1" ]]; then
  echo "  OK   GET /deploy-lock -> true (window open)"
else
  echo "  FAIL GET /deploy-lock never reported locked"; fail=1
fi

post_task
if [[ "$CODE" == "406" ]] && echo "$BODY" | jq -e '.status == "rejected" and .error == "lockdown is active, deployments are not accepted"' >/dev/null 2>&1; then
  echo "  OK   POST /tasks -> 406 rejected (deploys frozen)"
else
  echo "  FAIL POST /tasks during lockdown: code=${CODE} body=${BODY} (want 406 rejected)"; fail=1
fi

if [[ "$assert_ws" == "1" ]]; then
  ws_ok=0
  for _ in $(seq 1 16); do   # up to ~80s: watcher polls once a minute
    if grep -q '^MSG locked$' "$probe_out"; then ws_ok=1; break; fi
    sleep 5
  done
  if [[ "$ws_ok" == "1" ]]; then
    echo "  OK   WS client received the 'locked' broadcast on the schedule transition"
  else
    echo "  FAIL no 'locked' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"; fail=1
  fi
fi

# --- 4. Revert the schedule and prove deploys are accepted again ---------------
helm_apply   # values file only -> LOCKDOWN_SCHEDULE dropped -> unlocked
start_pf
if curl -s -m 10 "${base}/deploy-lock" | jq -e '. == false' >/dev/null 2>&1; then
  echo "  OK   GET /deploy-lock -> false after revert"
else
  echo "  FAIL GET /deploy-lock still true after revert"; fail=1
fi
post_task   # tokenless task is accepted (202) and skips write-back: no side effect
if [[ "$CODE" == "202" ]]; then
  echo "  OK   POST /tasks -> 202 accepted (deploys unfrozen)"
else
  echo "  FAIL POST /tasks after revert: code=${CODE} body=${BODY} (want 202)"; fail=1
fi

if [[ "$fail" -eq 0 ]]; then echo "LOCKDOWN: PASS"; else echo "LOCKDOWN: FAIL"; exit 1; fi
