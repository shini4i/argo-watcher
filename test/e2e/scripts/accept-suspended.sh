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

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="suspendapp"
# A tag different from the chart's revision-1 tag (v1.10.1) so the write-back
# triggers a second Rollout revision, which is what pauses at the canary step.
TAG="${TAG:-v1.10.2}"

bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
build_client "$bin_dir" || die "client build failed"

# Apply the Rollout fixture and wait for revision 1 to roll out Healthy (the
# initial revision skips the canary steps).
kubectl apply -f "${E2E_DIR}/fixtures/suspended-app.yaml"
require_app_synced "$APP" 60
echo "suspendapp revision 1 status: ${APP_STATE}"

wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> ${IMAGE}:${TAG} (write-back triggers a paused canary revision)"

# With a token the write-back bumps image.tag, triggering revision 2 -> canary
# pause -> Suspended. TASK_TIMEOUT covers write-back + sync + the rollout reaching
# the pause; a regression (Suspended not accepted) fails once the timeout elapses.
if run_client "$APP" "$TAG" ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN"; then
  echo "ACCEPT-SUSPENDED: PASS (paused Rollout accepted as deployed)"
  exit 0
fi
echo "ACCEPT-SUSPENDED: FAIL (client exited non-zero — paused Rollout was not accepted)"
exit 1
