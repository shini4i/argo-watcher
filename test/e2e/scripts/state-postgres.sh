#!/usr/bin/env bash
# Prove the Postgres-backed state path end to end.
#
# The rest of the lab runs argo-watcher on in-memory state; every functional phase
# before this one has already validated that backend. This phase flips the SAME
# release to STATE_TYPE=postgres (against the in-cluster Postgres from
# fixtures/postgres.yaml) and asserts the things only Postgres can demonstrate:
#
#   1. the chart's pre-upgrade migration Job applies the schema and the server
#      boots reporting state_type=postgres (GET /config),
#   2. a real authenticated deploy still drives the whole loop on Postgres —
#      task -> SSH write-back to Gitea -> Argo sync -> deployed,
#   3. THE payoff: the task record SURVIVES a server pod restart. On in-memory the
#      same restart loses all history (the task would 404); on Postgres it is
#      still there, 'deployed'. This is the whole reason the Postgres backend
#      exists, and nothing else in the suite (or the unit tests) asserts it.
#   4. supersession under real git contention works on Postgres — a newer deploy
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
# Optional env: DEPLOY_TOKEN, IMAGE, APP, PORT, GITEA_PORT, GITEA_ADMIN, GITEA_PW.
set -euo pipefail

NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
AW_CHART_REPO="${AW_CHART_REPO:?AW_CHART_REPO required (chart repo URL)}"
AW_CHART_VERSION="${AW_CHART_VERSION:?AW_CHART_VERSION required (pinned chart version)}"
IMAGE="${IMAGE:-traefik/whoami}"
# Persistence deploy runs against app4 — untouched by the earlier deploy phases
# (smoke/commit-format/race use app1/app3), so its newest task is unambiguously
# the one we create here.
APP="${APP:-app4}"
PORT="${PORT:-18098}"
GITEA_PORT="${GITEA_PORT:-13004}"
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
CLIENT_BIN="${bin_dir}/aw-client"
pf_pid=""
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT

base="http://localhost:${PORT}/api/v1"

# forward: (re)establish a port-forward to the argo-watcher service and block until
# /healthz answers. Called again after the restart, since deleting the pod kills
# the previous forward.
forward() {
  [[ -n "$pf_pid" ]] && kill "$pf_pid" 2>/dev/null || true
  kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
  pf_pid=$!
  for _ in $(seq 1 30); do
    curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && return 0
    sleep 1
  done
  echo "STATE-POSTGRES: FAIL — argo-watcher /healthz never came up on :${PORT}"
  exit 1
}

echo "=== provisioning in-cluster Postgres ==="
kubectl apply -f "${here}/../fixtures/postgres/"
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher-db --timeout=180s

echo "=== flipping the release to STATE_TYPE=postgres (runs the migration Job) ==="
# --wait blocks on the pre-upgrade migration hook AND the rolled StatefulSet; the
# hook (argo-watcher --migrate) applies the schema before the server starts.
helm upgrade --install argo-watcher argo-watcher --repo "$AW_CHART_REPO" \
  --version "$AW_CHART_VERSION" -n "$NS_AW" \
  -f "${here}/../values/argo-watcher.yaml" \
  -f "${here}/../values/argo-watcher-postgres.yaml" \
  --set image.tag=race --wait --timeout 5m
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s

# The completed hook Job lingers (hook-delete-policy: before-hook-creation), so
# assert it succeeded explicitly. Absence is tolerated: a green helm upgrade above
# already implies the hook ran to completion.
if kubectl -n "$NS_AW" get job/argo-watcher-migration >/dev/null 2>&1; then
  kubectl -n "$NS_AW" wait --for=condition=complete job/argo-watcher-migration --timeout=120s \
    || { echo "STATE-POSTGRES: FAIL — migration Job did not complete"; exit 1; }
  echo "  OK   migration Job completed"
fi

forward

echo "=== asserting the server is actually on Postgres ==="
st="$(curl -s -m 10 "${base}/config" | jq -r '.state_type')"
if [[ "$st" != "postgres" ]]; then
  echo "STATE-POSTGRES: FAIL — /config state_type=${st:-<none>}, want postgres"
  exit 1
fi
echo "  OK   /config reports state_type=postgres"

echo "=== deploying ${APP} on Postgres (real write-back loop) ==="
( cd "$root" && go build -o "$CLIENT_BIN" ./cmd/client ) \
  || { echo "STATE-POSTGRES: FAIL — client build failed"; exit 1; }

# Wait for the app's baseline sync, then deploy a tag different from its current one
# so the write-back actually commits (an unchanged tag is byte-compared and skipped).
for _ in $(seq 1 40); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "STATE-POSTGRES: FAIL — ${APP} never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }

cur=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.summary.images}' 2>/dev/null || true)
if [[ "$cur" == *v1.10.2* ]]; then TAG="v1.10.1"; else TAG="v1.10.2"; fi

if ! ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" IMAGE_TAG="${TAG}" ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e-pg" PROJECT_NAME="lab" \
   ARGO_WATCHER_DEPLOY_TOKEN="${DEPLOY_TOKEN}" \
   RETRY_INTERVAL="5s" TASK_TIMEOUT="180" \
   "$CLIENT_BIN"; then
  echo "STATE-POSTGRES: FAIL — deploy of ${APP}:${TAG} did not reach 'deployed'"
  exit 1
fi
echo "  OK   ${APP}:${TAG} deployed on Postgres"

# Capture the task we just created: the newest task for this app.
id=$(curl -s -m 10 "${base}/tasks?from_timestamp=0&app=${APP}" | jq -r '.tasks | sort_by(.created) | last | .id')
if [[ -z "$id" || "$id" == "null" ]]; then
  echo "STATE-POSTGRES: FAIL — could not read the created task id from the task list"
  exit 1
fi
echo "  task id=${id}"

echo "=== restarting the server; the task must survive (Postgres persistence) ==="
kubectl -n "$NS_AW" delete pod argo-watcher-0 --wait=true
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s
forward   # the previous forward died with the pod

code=$(curl -s -m 10 -o /dev/null -w '%{http_code}' "${base}/tasks/${id}")
status_after=$(curl -s -m 10 "${base}/tasks/${id}" | jq -r '.status')
if [[ "$code" != "200" || "$status_after" != "deployed" ]]; then
  echo "STATE-POSTGRES: FAIL — task ${id} did not survive the restart (http=${code} status=${status_after:-<none>})"
  echo "  (in-memory loses history here; Postgres must return 200 'deployed')"
  exit 1
fi
echo "  OK   task ${id} still present as 'deployed' after the restart"

echo "=== supersession under git contention on Postgres ==="
# race-supersede.sh is self-contained (resets its app to a baseline, runs its own
# competitor) and drives CancelInProgressTasks — the one deploy-flow query whose
# SQL differs from the in-memory path. Give it the already-forwarded API plus a
# Gitea forward for the commit check. Runs on app1, independent of app4 above.
# Wait for app1 to be Healthy first (as the `race` task does) so the baseline reset
# deploys from a known-good state — matters when this phase is run standalone.
for _ in $(seq 1 40); do
  [[ "$(kubectl -n argocd get application app1 -o jsonpath='{.status.health.status}' 2>/dev/null)" == "Healthy" ]] && break
  sleep 3
done
kubectl -n gitea port-forward svc/gitea-http "${GITEA_PORT}:3000" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${GITEA_PORT}" && break; sleep 1; done

if ! DEPLOY_TOKEN="${DEPLOY_TOKEN}" BASE_URL="http://localhost:${PORT}" \
   GITEA_REPO_URL="http://${GITEA_ADMIN}:${GITEA_PW}@localhost:${GITEA_PORT}/e2e/gitops.git" \
   "${here}/race-supersede.sh"; then
  echo "STATE-POSTGRES: FAIL — supersession under contention failed on Postgres"
  exit 1
fi

echo "STATE-POSTGRES: PASS (migrated, deployed, survived restart, superseded under contention)"
