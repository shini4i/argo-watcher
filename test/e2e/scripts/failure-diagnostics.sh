#!/usr/bin/env bash
# Assert argo-watcher extracts an ACTIONABLE failure reason from real ArgoCD — not just the
# image lists. Each scenario drives a deployment that is meant to fail a specific way, then
# checks the stored task `status_reason` contains the substrings that pin the diagnosis.
#
# Table-driven on purpose: adding coverage is one entry in SCENARIOS plus a scenario_<name>
# function. Each scenario echoes, on stdout, a series of "key=value" lines the runner reads:
#   task=<json>            deploy payload POSTed to /api/v1/tasks   (required)
#   token=<0|1>            send the deploy token (enables write-back); default 1
#   expect=<substring>     a substring that MUST appear in status_reason (repeatable)
#   setup / teardown       optional: names of functions run before/after the scenario
# The runner submits the task, waits for a terminal status, and greps the reason.
#
# Usage: DEPLOY_TOKEN=... failure-diagnostics.sh
set -uo pipefail

NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18095}"
IMAGE="${IMAGE:-traefik/whoami}"
GOOD_TAG="${GOOD_TAG:-v1.10.1}"
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
trap 'kill $(jobs -p) 2>/dev/null || true' EXIT
for _ in $(seq 1 15); do curl -s -m3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

# --- helpers ----------------------------------------------------------------
api() {
  local path="$1"; shift
  curl -s -m 15 "localhost:${PORT}/api/v1/${path}" "$@"
  return
}

# submit_task <json> <use_token> -> task id
submit_task() {
  local payload="$1" use_token="$2" hdr=()
  [[ "$use_token" == "1" ]] && hdr=(-H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}")
  # "${hdr[@]+...}" guards the empty-array expansion so it is safe under `set -u` on bash < 4.4.
  api tasks -X POST -H 'Content-Type: application/json' "${hdr[@]+"${hdr[@]}"}" -d "$payload" | jq -r '.id'
  return
}

# wait_terminal <id> -> prints final status
wait_terminal() {
  local id="$1" st
  for _ in $(seq 1 48); do
    st=$(api "tasks/${id}" | jq -r '.status // "?"')
    case "$st" in
      deployed|failed|aborted) echo "$st"; return 0 ;;
      *) ;; # non-terminal: keep polling
    esac
    sleep 5
  done
  echo "timeout"
  return
}

# restore_good_tag <app>: bump the app back to a pullable tag so the lab stays reusable.
restore_good_tag() {
  local app="$1" id
  id=$(submit_task "{\"app\":\"${app}\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":120,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${GOOD_TAG}\"}]}" 1)
  wait_terminal "$id" >/dev/null
  return
}

# --- scenarios --------------------------------------------------------------
# Committed image tag that cannot be pulled -> pods ImagePullBackOff. The cause lives ONLY on
# the Pod (resource tree), never in the app's top-level resources; the fix must surface it.
scenario_bad_image() {
  echo "task={\"app\":\"app1\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":90,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-nopull\"}]}"
  echo "token=1"
  echo "expect=Unhealthy resources:"
  echo "expect=Pod("
  echo "expect=ErrImagePull"
  echo "teardown=teardown_bad_image"
  return
}
teardown_bad_image() { restore_good_tag app1; return; }

# A deploy request whose image is never applied (unvalidated -> write-back skipped) stays
# "not available" with the app green. There is nothing for ArgoCD to diagnose, so the reason
# must be the baseline image lists WITHOUT inventing diagnostics.
scenario_unvalidated_not_available() {
  echo "task={\"app\":\"app2\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":45,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-never\"}]}"
  echo "token=0"
  echo "expect=Rollout status is not available"
  echo "expect=List of expected images:"
  return
}

# A failing PreSync hook (Helm Job) must surface as a failed hook, not just image lists.
#
# The tag MUST differ from app3's current live tag. argo-watcher only drives a sync when the
# write-back actually changes the override file (unchanged tags are a no-op since #472); a sync
# is what makes ArgoCD run the PreSync hook. The SOAK phase deterministically (fixed RNG seed)
# leaves app3 at one of ${TAGS} (v1.10.1/2/3), so we deploy a tag OUTSIDE that set to guarantee
# a real write-back regardless of which SOAK tag app3 ended on. The tag is never pulled: a failing
# PreSync hook aborts the sync before the main wave applies the Deployment — so the image stays at
# the old tag, the expected image is "not available", and the failure diagnostics carry the hook.
scenario_failed_presync_hook() {
  echo "task={\"app\":\"app3\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":90,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-hookfail\"}]}"
  echo "token=1"
  echo "expect=Failed hooks:"
  echo "expect=PreSync Failed"
  echo "setup=setup_failed_presync_hook"
  echo "teardown=teardown_failed_presync_hook"
  return
}
setup_failed_presync_hook()    { "${here}/hook-fixture.sh" add app3; return; }
teardown_failed_presync_hook() { "${here}/hook-fixture.sh" remove app3; restore_good_tag app3; return; }

SCENARIOS=(
  scenario_bad_image
  scenario_unvalidated_not_available
  scenario_failed_presync_hook
)

# --- runner -----------------------------------------------------------------
overall=0
for scenario in "${SCENARIOS[@]}"; do
  echo "=== ${scenario#scenario_} ==="
  spec="$($scenario)"
  task=$(sed -n 's/^task=//p' <<<"$spec")
  token=$(sed -n 's/^token=//p' <<<"$spec"); token="${token:-1}"
  setup=$(sed -n 's/^setup=//p' <<<"$spec")
  teardown=$(sed -n 's/^teardown=//p' <<<"$spec")
  mapfile -t expects < <(sed -n 's/^expect=//p' <<<"$spec")

  [[ -n "$setup" ]] && { echo "  setup: $setup"; "$setup"; }

  id=$(submit_task "$task" "$token")
  status=$(wait_terminal "$id")
  reason=$(api "tasks/${id}" | jq -r '.status_reason // ""')
  echo "  status=${status}"
  echo "  reason: $(sed 's/^/    | /' <<<"$reason")"

  ok=1
  [[ "$status" == "failed" ]] || { echo "  FAIL: expected terminal 'failed', got '${status}'"; ok=0; }
  for want in "${expects[@]}"; do
    if grep -qF -- "$want" <<<"$reason"; then
      echo "  OK: reason contains «${want}»"
    else
      echo "  FAIL: reason missing «${want}»"; ok=0
    fi
  done

  [[ -n "$teardown" ]] && { echo "  teardown: $teardown"; "$teardown"; }
  [[ "$ok" -eq 1 ]] || overall=1
done

if [[ "$overall" -eq 0 ]]; then echo "FAILURE-DIAGNOSTICS: PASS"; else echo "FAILURE-DIAGNOSTICS: FAIL"; exit 1; fi
