#!/usr/bin/env bash
# One authenticated end-to-end deploy against a fixture app: wait for its initial
# sync, bump its image tag, then wait for the task to reach "deployed". Proves the
# whole loop — task -> SSH write-back to Gitea -> Argo sync -> app Healthy on the
# new tag. Reproducible Phase A check (what previously had to be done by hand).
#
# Usage: smoke-deploy.sh [app] [tag]
set -euo pipefail

APP="${1:-app1}"
TAG="${2:-v1.10.2}"
IMAGE="${IMAGE:-traefik/whoami}"
NS_AW="${NS_AW:-argo-watcher}"
# Must match the ARGO_WATCHER_DEPLOY_TOKEN set in the argo-watcher secret;
# unauthenticated tasks are accepted but skip write-back (Validated=false).
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18090}"

# Wait for the app's initial sync so we deploy from a known-good baseline.
for _ in $(seq 1 40); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [ "$s" = "Synced/Healthy" ] && break
  sleep 5
done

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
trap 'kill $(jobs -p) 2>/dev/null || true' EXIT
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

id=$(curl -s -m 15 -X POST "localhost:${PORT}/api/v1/tasks" \
  -H 'Content-Type: application/json' -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}" \
  -d "{\"app\":\"${APP}\",\"author\":\"e2e\",\"project\":\"lab\",\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${TAG}\"}]}" \
  | jq -r '.id')
echo "task ${id}: deploying ${APP} -> ${IMAGE}:${TAG}"

for _ in $(seq 1 48); do
  st=$(curl -s -m 10 "localhost:${PORT}/api/v1/tasks/${id}" | jq -r '.status // "?"')
  case "$st" in
    deployed)       echo "OK: task deployed"; exit 0;;
    failed|aborted) echo "FAIL: task ${st}";  exit 1;;
  esac
  sleep 5
done
echo "FAIL: timed out waiting for terminal status"; exit 1
