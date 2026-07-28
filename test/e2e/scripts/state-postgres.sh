#!/usr/bin/env bash
# Prove the Postgres-backed state path end to end.
#
# The rest of the lab runs argo-watcher on in-memory state; every functional phase
# before this one has already validated that backend. This phase flips the SAME
# release to STATE_TYPE=postgres (against the in-cluster Postgres from
# fixtures/postgres/) and asserts the things only Postgres can demonstrate:
#
#   1. the chart's pre-upgrade migration Job applies the schema and the server
#      boots reporting state_type=postgres (GET /config),
#   2. a real authenticated deploy still drives the whole loop on Postgres —
#      task -> SSH write-back to Gitea -> Argo sync -> deployed,
#   3. THE payoff: the task record SURVIVES a server pod restart. On in-memory the
#      same restart loses all history (the task would 404); on Postgres it is
#      still there, 'deployed'. This is the whole reason the Postgres backend
#      exists, and nothing else in the suite (or the unit tests) asserts it.
#   4. the deploy lock is SHARED and PERSISTENT: a lock row written by another
#      writer (what a second replica's SetLock does) is honored by this server —
#      GET reports it, deploys are rejected, and the lockdown watcher broadcasts it
#      to a connected WebSocket client (the only path a browser has to a lock set
#      elsewhere, since the API handlers do not push) — it survives a pod restart,
#      and clearing it unblocks deploys again. On in-memory state the lock never
#      leaves the process that set it and dies with it.
#   5. supersession under real git contention works on Postgres — a newer deploy
#      cancels an older retrying one via CancelInProgressTasks (hand-written SQL
#      that DIFFERS from the in-memory Go path) and the superseded task never
#      clobbers the winner's write-back. This guards git-op correctness on the
#      Postgres backend specifically.
#
# Runs BEFORE failure-diagnostics so it deploys against pristine fixture apps
# (that phase dirties app tags and only best-effort restores them). Everything
# after it (failure-diagnostics, shutdown-drain) then runs on Postgres too — both
# are backend-agnostic assertions, so that is a free bonus, not lost coverage.
#
# Required env: AW_CHART_REPO, AW_CHART_VERSION (to helm-upgrade the release).
# Optional env: DEPLOY_TOKEN, IMAGE, APP.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

AW_CHART_REPO="${AW_CHART_REPO:?AW_CHART_REPO required (chart repo URL)}"
AW_CHART_VERSION="${AW_CHART_VERSION:?AW_CHART_VERSION required (pinned chart version)}"
# Persistence deploy runs against app4 — untouched by the earlier deploy phases
# (smoke/commit-format/race use app1/app3), so its newest task is unambiguously
# the one we create here.
APP="${APP:-app4}"

bin_dir="$(mktemp -d)"
probe_out="$(mktemp)"
probe_pid=""
# Set to 1 while the deploy-lock assertion holds the shared lock (see below).
lock_set=0

psql_db() {
  local sql="$1"
  kubectl -n "$NS_AW" exec argo-watcher-db-0 -- psql -qtAX -U argo_watcher -d argo_watcher -c "$sql"
}

cleanup() {
  # The deploy lock lives in the shared database, so a lock left set by an
  # aborted run would 406 every deploy in the phases that follow. Always drop it.
  if [[ "$lock_set" == 1 ]]; then
    psql_db "UPDATE deploy_lock SET manual_lock = false" >/dev/null 2>&1 || true
  fi
  # jobs -p prints one PID per line; splitting them into separate arguments is
  # the point, and they are always bare digits.
  # shellcheck disable=SC2046
  kill $(jobs -p) 2>/dev/null || true
  rm -rf "$bin_dir" "$probe_out"
}
trap cleanup EXIT

# start_probe: hold a WebSocket client open and block until it has connected, so a
# broadcast triggered afterwards cannot be missed by a slow handshake. A pod
# restart kills the socket, so call this again after each restart. Any previous
# probe is killed first: it would otherwise keep appending to the same file that
# this one truncates, mixing the old capture into the new assertion.
start_probe() {
  [[ -n "$probe_pid" ]] && kill "$probe_pid" 2>/dev/null || true
  : >"$probe_out"
  WS_URL="$AW_WS_URL" DURATION=300s "${bin_dir}/wsprobe" >"$probe_out" 2>/dev/null &
  probe_pid=$!
  wait_ws_open "$probe_out" || die "WS probe never connected on ${AW_WS_URL}"
}

echo "=== provisioning in-cluster Postgres ==="
kubectl apply -f "${E2E_DIR}/fixtures/postgres/"
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher-db --timeout=180s

echo "=== flipping the release to STATE_TYPE=postgres (runs the migration Job) ==="
# Not helm_apply_aw: this apply layers a SECOND values file (the postgres overlay)
# and needs --wait to block on the pre-upgrade migration hook, which
# helm_apply_aw's single-file + --reset-values shape does not cover.
helm upgrade --install argo-watcher argo-watcher --repo "$AW_CHART_REPO" \
  --version "$AW_CHART_VERSION" -n "$NS_AW" \
  -f "${E2E_DIR}/values/argo-watcher.yaml" \
  -f "${E2E_DIR}/values/argo-watcher-postgres.yaml" \
  --set image.tag=race --wait --timeout 5m
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s

# The completed hook Job lingers (hook-delete-policy: before-hook-creation), so
# assert it succeeded explicitly. Absence is tolerated: a green helm upgrade above
# already implies the hook ran to completion.
if kubectl -n "$NS_AW" get job/argo-watcher-migration >/dev/null 2>&1; then
  kubectl -n "$NS_AW" wait --for=condition=complete job/argo-watcher-migration --timeout=120s \
    || die "migration Job did not complete"
  ok "migration Job completed"
fi

wait_service || die "argo-watcher /healthz never came up on ${AW_URL}"

echo "=== asserting the server is actually on Postgres ==="
st="$(curl -s -m 10 "${AW_API}/config" | jq -r '.state_type')"
[[ "$st" == "postgres" ]] || die "/config state_type=${st:-<none>}, want postgres"
ok "/config reports state_type=postgres"

echo "=== deploying ${APP} on Postgres (real write-back loop) ==="
build_client "$bin_dir" || die "client build failed"
build_bin "$bin_dir" ./test/e2e/tools/wsprobe || die "wsprobe build failed"

# Wait for the app's baseline sync, then deploy a tag different from its current one
# so the write-back actually commits (an unchanged tag is byte-compared and skipped).
require_app_synced "$APP"
TAG="$(other_tag "$APP")"

run_client "$APP" "$TAG" \
  COMMIT_AUTHOR="e2e-pg" \
  ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" \
  || die "deploy of ${APP}:${TAG} did not reach 'deployed'"
ok "${APP}:${TAG} deployed on Postgres"

# Capture the task we just created: the newest task for this app.
id=$(curl -s -m 10 "${AW_API}/tasks?from_timestamp=0&app=${APP}" | jq -r '.tasks | sort_by(.created) | last | .id')
[[ -n "$id" && "$id" != "null" ]] || die "could not read the created task id from the task list"
echo "  task id=${id}"

echo "=== restarting the server; the task must survive (Postgres persistence) ==="
kubectl -n "$NS_AW" delete pod argo-watcher-0 --wait=true
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s
# No forward to re-establish: the NodePort URL is unaffected by the pod swap.
wait_service || die "argo-watcher /healthz never came back after the restart"

req GET "${AW_API}/tasks/${id}"
status_after=$(jq -r '.status' <<<"$BODY")
if [[ "$CODE" != "200" || "$status_after" != "deployed" ]]; then
  echo "  (in-memory loses history here; Postgres must return 200 'deployed')"
  die "task ${id} did not survive the restart (http=${CODE} status=${status_after:-<none>})"
fi
ok "task ${id} still present as 'deployed' after the restart"

echo "=== shared deploy lock: a lock written by another writer is honored ==="
# The manual lock API needs OIDC (heavy tier), so write the shared row directly —
# which is exactly what a second replica's SetLock does. This asserts the whole
# read path on Postgres: the migration created deploy_lock, the server reads it
# per request, and a lock nobody set on THIS process still rejects deployments.
# The EXIT trap clears the lock, so an abort mid-assertion cannot freeze the
# release for the phases that follow.
#
# A WS client is held open across the write: since the API handlers do not push,
# the lockdown watcher is the ONLY thing that tells a browser about a lock set
# elsewhere. Nothing else in the lab observes that path — lockdown.sh covers the
# schedule trigger, and that sub-check is skipped when the pod boots in-window.
start_probe
psql_db "UPDATE deploy_lock SET manual_lock = true, override_until = NULL" >/dev/null
lock_set=1

curl -s -m 10 "${AW_API}/deploy-lock" | jq -e '. == true' >/dev/null 2>&1 \
  || die "GET /deploy-lock did not report the shared lock"

wait_ws "$probe_out" locked \
  || die "no 'locked' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"
ok "the watcher broadcast 'locked' for a lock written by another writer"

post_task "$(task_json "$APP" v1.10.3 e2e-pg)" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}"
[[ "$CODE" == "406" ]] || die "deploy during a shared lock returned ${CODE}, want 406"
ok "shared lock rejects deployments (406) on a replica that never set it"

# The lock must also OUTLIVE the process that would have held it in memory: the
# old implementation lost it on restart, which is the other half of why it moved
# into the database. A fresh pod reads the row on its first request.
echo "  restarting the server; the lock must still be in effect"
kubectl -n "$NS_AW" delete pod argo-watcher-0 --wait=true
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s
wait_service || die "argo-watcher /healthz never came back after the second restart"

curl -s -m 10 "${AW_API}/deploy-lock" | jq -e '. == true' >/dev/null 2>&1 \
  || die "the shared lock did not survive the restart"

post_task "$(task_json "$APP" v1.10.3 e2e-pg)" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}"
[[ "$CODE" == "406" ]] || die "deploy after the restart returned ${CODE}, want 406 (lock lost)"
ok "the lock survived the restart and still rejects deployments"

# The restart above killed the previous socket, so the release needs a probe
# reconnected to the new pod.
start_probe
psql_db "UPDATE deploy_lock SET manual_lock = false" >/dev/null
lock_set=0
curl -s -m 10 "${AW_API}/deploy-lock" | jq -e '. == false' >/dev/null 2>&1 \
  || die "GET /deploy-lock still locked after the shared release"
ok "releasing the shared lock unblocks deployments again"

wait_ws "$probe_out" unlocked \
  || die "no 'unlocked' WS broadcast (captured: $(tr '\n' ',' <"$probe_out"))"
ok "the watcher broadcast 'unlocked' for the shared release"

echo "=== supersession under git contention on Postgres ==="
# race-supersede.sh drives CancelInProgressTasks — the one deploy-flow query whose
# SQL differs from the in-memory path. It is self-contained (waits for its app,
# resets it to a baseline, runs its own competitor) and runs on app1, independent of
# app4 above; reuses the client binary built here.
CLIENT_BIN="$CLIENT_BIN" DEPLOY_TOKEN="$DEPLOY_TOKEN" "${here}/race-supersede.sh" \
  || die "supersession under contention failed on Postgres"

echo "STATE-POSTGRES: PASS (migrated, deployed, survived restart, shared deploy lock, superseded under contention)"
