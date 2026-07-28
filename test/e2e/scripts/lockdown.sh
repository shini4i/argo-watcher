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
# than flaking.
#
# A manual (Keycloak) lock/unlock is NOT a separate trigger: the API handlers do
# not push, so it reaches clients through this same lockdown watcher and the same
# one-poll-interval delay. state-postgres.sh asserts that path over the WS for a
# lock written by another writer; TestDeployLockNotifiedOnlyByWatcher pins that the
# handlers stay silent.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

# Required by helm_apply_aw; asserted here so a missing value fails on line 1 rather
# than part-way through the schedule arithmetic.
: "${AW_CHART_REPO:?AW_CHART_REPO is required}" "${AW_CHART_VERSION:?AW_CHART_VERSION is required}"
# Not configurable: extra_envs_index must count entries in the SAME file
# helm_apply_aw applies, or the appended --set would overwrite a real entry.
VALUES="${E2E_DIR}/values/argo-watcher.yaml"

bin_dir="$(mktemp -d)"
probe_out="$(mktemp)"
cleanup() {
  # jobs -p prints one PID per line; splitting them into separate arguments is the
  # point, and they are always bare digits.
  # shellcheck disable=SC2046
  kill $(jobs -p) 2>/dev/null || true
  rm -rf "$bin_dir" "$probe_out"
}
trap cleanup EXIT
build_bin "$bin_dir" ./test/e2e/tools/wsprobe || die "wsprobe build failed"
wsprobe="$BIN"

# A tokenless task: accepted (202) and skips write-back, so the checks below have no
# lasting side effect.
TASK="$(task_json app1 v1.10.2)"

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

# Append LOCKDOWN_SCHEDULE as the next free extraEnvs entry (see extra_envs_index
# in lib.sh for why the index is counted from the values file).
idx=$(extra_envs_index "$VALUES")
helm_apply_aw --set-string "extraEnvs[${idx}].name=LOCKDOWN_SCHEDULE" \
              --set-string "extraEnvs[${idx}].value=${schedule}"
wait_service || die "argo-watcher never came back after enabling the schedule"

# lock_is <true|false> -> succeeds when GET /deploy-lock equals it.
lock_is() {
  curl -s -m 10 "${AW_API}/deploy-lock" | jq -e ". == $1" >/dev/null 2>&1
}

# --- 2. Confirm we booted pre-window, then hold a WS client open ---------------
assert_ws=1
if lock_is false; then
  ok "booted unlocked (pre-window); watching for the scheduled 'locked' broadcast"
  WS_URL="$AW_WS_URL" DURATION=400s "$wsprobe" >"$probe_out" 2>/dev/null &
  # The window is still ~180s out, so the default ~20s connect window is ample.
  if ! wait_ws_open "$probe_out"; then
    bad "WS probe never connected before the window opened"
    assert_ws=0
  fi
else
  note "rollout booted in-window; skipping the WS-transition sub-check (not a failure)"
  assert_ws=0
fi

# --- 3. Wait for the window to open, then assert the locked behaviour ----------
# Up to ~300s: the window opens at +180s and is then evaluated live per request.
if retry 60 5 lock_is true; then
  ok "GET /deploy-lock -> true (window open)"
else
  bad "GET /deploy-lock never reported locked"
fi

post_task "$TASK"
if [[ "$CODE" == "406" ]] &&
   jq -e '.status == "rejected" and .error == "lockdown is active, deployments are not accepted"' <<<"$BODY" >/dev/null 2>&1; then
  ok "POST /tasks -> 406 rejected (deploys frozen)"
else
  bad "POST /tasks during lockdown: code=${CODE} body=${BODY} (want 406 rejected)"
fi

if [[ "$assert_ws" == "1" ]]; then
  # Up to ~80s: the watcher polls once a minute.
  if wait_ws "$probe_out" locked 16; then
    ok "WS client received the 'locked' broadcast on the schedule transition"
  else
    bad "no 'locked' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"
  fi
fi

# --- 4. Revert the schedule and prove deploys are accepted again ---------------
helm_apply_aw   # values file only -> LOCKDOWN_SCHEDULE dropped -> unlocked
wait_service || die "argo-watcher never came back after reverting the schedule"
if lock_is false; then
  ok "GET /deploy-lock -> false after revert"
else
  bad "GET /deploy-lock still true after revert"
fi
post_task "$TASK"
if [[ "$CODE" == "202" ]]; then
  ok "POST /tasks -> 202 accepted (deploys unfrozen)"
else
  bad "POST /tasks after revert: code=${CODE} body=${BODY} (want 202)"
fi

phase_end LOCKDOWN
