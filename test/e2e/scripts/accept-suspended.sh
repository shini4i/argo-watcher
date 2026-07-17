#!/usr/bin/env bash
# Prove ACCEPT_SUSPENDED_APP against its real case: an argo-rollouts Rollout that
# pauses mid-rollout. suspendapp is a managed Rollout with a canary pause step.
# Revision 1 rolls out Healthy; the deploy below writes back a new image tag, which
# triggers revision 2 — the canary scales the new image up (so it is live and in
# the app's summary images) and then pauses, so ArgoCD reports the app Suspended
# while the pod keeps running.
#
# With ACCEPT_SUSPENDED_APP=true argo-watcher treats that Synced+Suspended state as
# a successful rollout and the client reaches "deployed" (exit 0). Without it the
# app would count as not-healthy and the deploy would time out waiting for a manual
# promotion. Exit 0 against a paused Rollout is the discriminating evidence.
#
# Usage: DEPLOY_TOKEN=... accept-suspended.sh
set -euo pipefail

APP="suspendapp"
IMAGE="${IMAGE:-traefik/whoami}"
# A tag different from the chart's revision-1 tag (v1.10.1) so the write-back
# triggers a second Rollout revision, which is what pauses at the canary step.
TAG="${TAG:-v1.10.2}"
NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18098}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the Rollout fixture and wait for revision 1 to roll out Healthy (the
# initial revision skips the canary steps).
kubectl apply -f "${here}/../fixtures/suspended-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "ACCEPT-SUSPENDED: FAIL — ${APP} rev1 never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }
echo "suspendapp revision 1 status: ${s:-unknown}"

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${TAG} (write-back triggers a paused canary revision)"

# With a token the write-back bumps image.tag, triggering revision 2 -> canary
# pause -> Suspended. TASK_TIMEOUT covers write-back + sync + the rollout reaching
# the pause; a regression (Suspended not accepted) fails once the timeout elapses.
if ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   ARGO_WATCHER_DEPLOY_TOKEN="${DEPLOY_TOKEN}" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="180" \
   "${bin_dir}/aw-client"; then
  echo "ACCEPT-SUSPENDED: PASS (paused Rollout accepted as deployed)"
  exit 0
fi
echo "ACCEPT-SUSPENDED: FAIL (client exited non-zero — paused Rollout was not accepted)"
exit 1
