#!/usr/bin/env bash
# Supersede authority: an uncredentialed deploy must not cancel a credentialed one.
#
# POST /api/v1/tasks accepts a task without a credential by design (it is then only
# tracked, never written back). Superseding (#353) turned that into a write on other
# deployments: an anonymous task matching a credentialed one on app + image NAME
# cancelled it, and the cancelled task's pending git write-back aborted with
# ErrDeploymentSuperseded — so its tag never landed. Asserts:
#   * an anonymous task is still accepted (202): the documented behaviour is intact,
#   * it does NOT cancel the credentialed deploy in flight,
#   * an anonymous task still supersedes another anonymous one, so token-less setups
#     keep the #353 behaviour,
#   * the credentialed deploy still reaches 'deployed' with both anons in flight,
#   * a credentialed task DOES supersede an anonymous one.
# The credentialed-supersedes-credentialed direction is covered by race-supersede.sh.
#
# Self-contained and order-independent: it needs APP Synced/Healthy, derives its own
# tags, and drains its own tasks so nothing is left in flight for collect.sh's gates.
# It leaves APP on TAG_A. Optional env: DEPLOY_TOKEN, APP, TAG_A, TAG_B, TAG_C.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="${APP:-app2}"
# TAG_B/TAG_C are never pulled: an anonymous task performs no write-back, so ArgoCD
# is never asked to sync to them.
TAG_B="${TAG_B:-v0.0.0-anon-b}"
TAG_C="${TAG_C:-v0.0.0-anon-c}"

require_app_synced "$APP"
wait_service || die "argo-watcher not reachable on ${AW_URL}"

# TAG_A must differ from the tag APP currently runs, or its write-back is a no-op
# fast path (unchanged tags skip write-back since #472): the task would reach
# 'deployed' on the first poll and the supersede attempts below would land on a
# task already terminal — passing even with the authority check removed. other_tag
# always returns something different from the live tag, and is read only now that
# the app is synced. Derived rather than fixed because this phase leaves APP on
# TAG_A, so a second invocation (state-postgres.sh) must not reuse the same value.
TAG_A="${TAG_A:-$(other_tag "$APP")}"

if [[ "$TAG_A" == "$TAG_B" || "$TAG_A" == "$TAG_C" || "$TAG_B" == "$TAG_C" ]]; then
  die "TAG_A/TAG_B/TAG_C must all differ (a=${TAG_A} b=${TAG_B} c=${TAG_C})"
fi

# submit <tag> [curl-args...]: POST a task and echo its id, dying on a non-202.
submit() {
  local tag="$1" id
  shift
  post_task "$(task_json "$APP" "$tag")" "$@"
  [[ "$CODE" == "202" ]] || die "POST /tasks for ${tag} returned ${CODE}, expected 202 (body=${BODY})"
  id=$(jq -r '.id // empty' <<<"$BODY")
  [[ -n "$id" ]] || die "no task id returned for ${tag} (body=${BODY})"
  echo "$id"
  return
}

# task_status <id>: the task's current status, "?" if unreadable.
task_status() {
  local id="$1"
  curl -s -m 10 "${AW_API}/tasks/${id}" | jq -r '.status // "?"'
  return
}

# refute_cancelled <id> <label>: poll for ~15s and fail if the task is ever
# cancelled. Polling rather than sampling once because supersession is applied by
# the request that follows, and the status is read from shared state. An unreadable
# status fails too: treating "?" as "not cancelled" would let an unreachable API
# satisfy the refutation.
refute_cancelled() {
  local id="$1" label="$2" i status
  for ((i = 1; i <= 8; i++)); do
    status=$(task_status "$id")
    case "$status" in
      cancelled)
        bad "${label} was cancelled by an uncredentialed task"
        return 1
        ;;
      '?')
        bad "${label} status was unreadable; cannot refute cancellation"
        return 1
        ;;
      *) ;;
    esac
    sleep 2
  done
  ok "${label} was not cancelled (status=${status})"
  return 0
}

echo "=== credentialed deploy in flight, then anonymous supersede attempts ==="
victim=$(submit "$TAG_A" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}") || exit 1
echo "  credentialed task ${victim}: ${APP} -> ${IMAGE}:${TAG_A}"

anon_b=$(submit "$TAG_B") || exit 1
echo "  anonymous task ${anon_b}: ${APP} -> ${IMAGE}:${TAG_B} (accepted, must not supersede)"

# Supersession runs synchronously inside AddTask, so the regression shows up here:
# by the time the anonymous POST has returned, an unguarded server has already
# cancelled the credentialed task. Report that as the failure it is; only a genuinely
# vacuous state (already terminal for another reason) is a lost precondition.
victim_now=$(task_status "$victim")
case "$victim_now" in
  "in progress") ;;
  cancelled)
    bad "the credentialed task was cancelled by the uncredentialed submission"
    phase_end SUPERSEDE-AUTHORITY
    ;;
  *)
    die "the credentialed task was '${victim_now}' when the anonymous task was submitted; nothing was in flight to supersede"
    ;;
esac

refute_cancelled "$victim" "the credentialed task"

echo "=== an anonymous task still supersedes another anonymous one (#353 intact) ==="
anon_c=$(submit "$TAG_C") || exit 1
echo "  anonymous task ${anon_c}: ${APP} -> ${IMAGE}:${TAG_C}"

anon_cancelled() {
  [[ "$(task_status "$anon_b")" == "cancelled" ]]
  return
}
if retry 10 2 anon_cancelled; then
  ok "the earlier anonymous task was superseded by the newer anonymous one"
else
  bad "an anonymous task no longer supersedes another anonymous one (status=$(task_status "$anon_b")) — token-less setups regressed"
fi

# The newer anonymous task must not have reached the credentialed one either — but
# only assert that while the victim is still in flight. Once it is terminal the
# refutation is vacuous, and claiming it would advertise a check the run never made.
victim_now=$(task_status "$victim")
if [[ "$victim_now" == "in progress" ]]; then
  refute_cancelled "$victim" "the credentialed task (after a second anonymous task)"
else
  note "the credentialed task was already '${victim_now}'; skipping the second refutation"
fi

echo "=== the credentialed deploy still completes ==="
victim_terminal() { case "$(task_status "$victim")" in deployed | failed | aborted | cancelled) return 0 ;; *) return 1 ;; esac; }
retry 48 5 victim_terminal
final=$(task_status "$victim")
[[ "$final" == "deployed" ]] \
  || bad "the credentialed deploy ended '${final}', expected 'deployed' (its write-back must survive the anonymous tasks)"

echo "=== a credentialed task supersedes an uncredentialed one (and drains the lab) ==="
# anon_c is still in flight and never rolls out (no write-back), so it would sit
# there until DEPLOYMENT_TIMEOUT and then trip collect.sh's failed_deployment and
# in_progress_tasks gates. Re-deploying TAG_A with a credential clears it: the tag is
# already live so the write-back is a no-op fast path, and a credentialed task may
# supersede an uncredentialed one — which is the last direction left to assert.
cleanup=$(submit "$TAG_A" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}") || exit 1
anon_c_cancelled() {
  [[ "$(task_status "$anon_c")" == "cancelled" ]]
  return
}
if retry 10 2 anon_c_cancelled; then
  ok "the anonymous task was superseded by a credentialed one"
else
  bad "a credentialed task did not supersede the anonymous one (status=$(task_status "$anon_c"))"
fi

cleanup_terminal() { case "$(task_status "$cleanup")" in deployed | failed | aborted | cancelled) return 0 ;; *) return 1 ;; esac; }
retry 24 5 cleanup_terminal
cleanup_final=$(task_status "$cleanup")
[[ "$cleanup_final" == "deployed" ]] \
  || note "cleanup deploy ended '${cleanup_final}'; nothing should remain in flight for ${APP}"

phase_end SUPERSEDE-AUTHORITY
