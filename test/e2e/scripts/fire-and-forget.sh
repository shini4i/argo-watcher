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

APP="ffapp"
IMAGE="${IMAGE:-traefik/whoami}"
# A tag different from the chart's default (v1.10.1) so the write-back is a real
# change; never actually runs (no pod until the far-future schedule).
TAG="${TAG:-v1.10.2}"
NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18095}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the managed CronJob fixture and wait for its initial sync. A CronJob app
# becomes Synced/Healthy once the CronJob exists (no pod required).
kubectl apply -f "${here}/../fixtures/fire-and-forget-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "FIRE-AND-FORGET: FAIL — ${APP} never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} (managed CronJob) -> ${IMAGE}:${TAG} in fire-and-forget mode"

# With the token the write-back bumps the CronJob's image tag. TASK_TIMEOUT is kept
# short so a regression (fire-and-forget NOT honoured) fails fast at "not available"
# rather than hanging for the full default window.
if ! ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   ARGO_WATCHER_DEPLOY_TOKEN="${DEPLOY_TOKEN}" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="60" \
   "${bin_dir}/aw-client"; then
  echo "FIRE-AND-FORGET: FAIL (client exited non-zero — the rollout wait was NOT skipped)"
  exit 1
fi

# Confirm the write-back actually reached the tracked workload: the live CronJob
# must now run the deployed tag (ArgoCD synced argo-watcher's override).
for _ in $(seq 1 20); do
  img=$(kubectl -n "$APP" get cronjob ffapp-cron -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].image}' 2>/dev/null || true)
  [[ "$img" == "${IMAGE}:${TAG}" ]] && break
  sleep 3
done
if [[ "$img" == "${IMAGE}:${TAG}" ]]; then
  echo "FIRE-AND-FORGET: PASS (write-back updated the CronJob to ${TAG}; deploy reported done without the image running)"
  exit 0
fi
echo "FIRE-AND-FORGET: FAIL (CronJob image is '${img:-<none>}', expected ${IMAGE}:${TAG} — write-back did not land)"
exit 1
