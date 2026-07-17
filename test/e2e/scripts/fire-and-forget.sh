#!/usr/bin/env bash
# Prove the argo-watcher/fire-and-forget annotation: a deploy to such an app skips
# the rollout health check and reports success immediately, without waiting for the
# app to actually run the requested image.
#
# The assertion deploys a tag the app can NEVER become healthy on (a nonexistent
# tag) and sends NO deploy token, so nothing is written back and the gitops repo
# stays clean. A normal app would poll that tag until TASK_TIMEOUT and fail; the
# fire-and-forget app reports "deployed" on the first check (client exit 0). Exit 0
# on an impossible tag is therefore the discriminating evidence that the rollout
# check was skipped.
#
# Usage: fire-and-forget.sh
set -euo pipefail

APP="ffapp"
IMAGE="${IMAGE:-traefik/whoami}"
# A tag that does not exist, so it could never pass a real rollout check.
BOGUS_TAG="${BOGUS_TAG:-v0.0.0-does-not-exist}"
NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18095}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the dedicated fire-and-forget fixture and wait for its initial sync so we
# deploy from a known-Healthy baseline (on the chart-default tag).
kubectl apply -f "${here}/../fixtures/fire-and-forget-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${BOGUS_TAG} (impossible tag) in fire-and-forget mode"

# No deploy token: unvalidated task, no write-back, repo untouched. TASK_TIMEOUT is
# kept short so a regression (checks NOT skipped) fails fast instead of hanging.
if ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${BOGUS_TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="60" \
   "${bin_dir}/aw-client"; then
  echo "FIRE-AND-FORGET: PASS (reached 'deployed' on an impossible tag — rollout check skipped)"
  exit 0
fi
echo "FIRE-AND-FORGET: FAIL (client exited non-zero — rollout check was NOT skipped)"
exit 1
