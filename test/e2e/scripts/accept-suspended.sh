#!/usr/bin/env bash
# Prove ACCEPT_SUSPENDED_APP: with it enabled, argo-watcher treats a Synced app
# whose health is "Suspended" as a successful rollout instead of waiting for it to
# become Healthy. suspendapp's only workload is a paused Deployment, so ArgoCD
# reports the app Suspended.
#
# We deploy the image the app already carries (no token, no write-back) so the
# image check passes and the rollout status reaches the Suspended branch. With
# ACCEPT_SUSPENDED_APP=true the client reaches "deployed" (exit 0); without it the
# app would count as not-healthy and the deploy would time out. Exit 0 against a
# Suspended app is the discriminating evidence.
#
# Usage: accept-suspended.sh
set -euo pipefail

APP="suspendapp"
IMAGE="${IMAGE:-traefik/whoami}"
# Must match the image tag in fixtures/suspended/deployment.yaml.
TAG="${TAG:-v1.10.1}"
NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18098}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the paused-Deployment fixture and wait for ArgoCD to report it
# Synced + Suspended (the state acceptSuspended handles).
kubectl apply -f "${here}/../fixtures/suspended-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Suspended" ]] && break
  sleep 5
done
echo "suspendapp status: ${s:-unknown}"

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${TAG} against a Suspended app"

# No deploy token: unvalidated, no write-back. TASK_TIMEOUT kept short so a
# regression (Suspended NOT accepted) fails fast instead of hanging.
if ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="60" \
   "${bin_dir}/aw-client"; then
  echo "ACCEPT-SUSPENDED: PASS (Suspended app accepted as deployed)"
  exit 0
fi
echo "ACCEPT-SUSPENDED: FAIL (client exited non-zero — Suspended app was not accepted)"
exit 1
