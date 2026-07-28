#!/usr/bin/env bash
# Prove argo-watcher/fire-and-forget end to end on a MANAGED CronJob app. ffapp's
# only workload is a CronJob (image tag a Helm value argo-watcher writes back). A
# CronJob has no long-running pod, so a freshly-deployed image is never observed
# rolling out and a normal deploy would wait out the timeout; fire-and-forget makes
# argo-watcher report success immediately.
#
# We deploy a new tag WITH the token, so argo-watcher genuinely writes it back and
# ArgoCD updates the CronJob to it. Assertions:
#   1. the client reaches "deployed" (exit 0) — fire-and-forget skipped the wait
#   2. the write-back actually landed: the live CronJob now runs the new tag
# Together they show the deploy really updated the tracked workload AND that
# success came without the image ever running (a CronJob's image never enters the
# app's summary, so without fire-and-forget this would time out at "not available").
#
# Usage: DEPLOY_TOKEN=... fire-and-forget.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="ffapp"
# A tag different from the chart's default (v1.10.1) so the write-back is a real
# change; never actually runs (no pod until the far-future schedule).
TAG="${TAG:-v1.10.2}"

bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
build_client "$bin_dir" || die "client build failed"

# Apply the managed CronJob fixture and wait for its initial sync. A CronJob app
# becomes Synced/Healthy once the CronJob exists (no pod required).
kubectl apply -f "${E2E_DIR}/fixtures/fire-and-forget-app.yaml"
require_app_synced "$APP" 60
wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} (managed CronJob) -> ${IMAGE}:${TAG} in fire-and-forget mode"

# With the token the write-back bumps the CronJob's image tag. TASK_TIMEOUT is kept
# short so a regression (fire-and-forget NOT honoured) fails fast at "not available"
# rather than hanging for the full default window.
if ! run_client "$APP" "$TAG" \
     ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" \
     TASK_TIMEOUT="60"; then
  echo "FIRE-AND-FORGET: FAIL (client exited non-zero — the rollout wait was NOT skipped)"
  exit 1
fi

# Confirm the write-back actually reached the tracked workload: the live CronJob
# must now run the deployed tag (ArgoCD synced argo-watcher's override).
cronjob_on_tag() {
  img=$(kubectl -n "$APP" get cronjob ffapp-cron \
    -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].image}' 2>/dev/null || true)
  [[ "$img" == "${IMAGE}:${TAG}" ]]
}
if retry 20 3 cronjob_on_tag; then
  echo "FIRE-AND-FORGET: PASS (write-back updated the CronJob to ${TAG}; deploy reported done without the image running)"
  exit 0
fi
echo "FIRE-AND-FORGET: FAIL (CronJob image is '${img:-<none>}', expected ${IMAGE}:${TAG} — write-back did not land)"
exit 1
