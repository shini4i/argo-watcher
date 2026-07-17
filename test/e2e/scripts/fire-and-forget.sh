#!/usr/bin/env bash
# Prove the argo-watcher/fire-and-forget annotation against its real use case: an
# app whose only workload is a CronJob. Such an app runs no pod until its schedule
# fires, so a freshly-deployed image tag is never observed "running" and a normal
# deploy would wait out the timeout reporting the image as not-yet-available.
# fire-and-forget makes the deploy report success immediately instead.
#
# ffapp deploys a CronJob on an effectively-never schedule (see the fixture), so no
# job runs during the phase. We deploy a real, valid tag (v1.10.2) that the app
# will never actually roll out, with NO deploy token (no write-back, gitops repo
# untouched). Without fire-and-forget this would poll until TASK_TIMEOUT and fail;
# with it, the client reaches "deployed" (exit 0). Exit 0 on a tag that never rolls
# out is the discriminating evidence that the rollout wait was skipped.
#
# Usage: fire-and-forget.sh
set -euo pipefail

APP="ffapp"
IMAGE="${IMAGE:-traefik/whoami}"
# A real tag the CronJob-only app never actually runs (no pod until the schedule
# fires), so it is never observed as rolled out.
TAG="${TAG:-v1.10.2}"
NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18095}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the dedicated CronJob-only fixture and wait for its initial sync. A
# CronJob app becomes Synced/Healthy once the CronJob exists (no pod required).
kubectl apply -f "${here}/../fixtures/fire-and-forget-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} (CronJob-only) -> ${IMAGE}:${TAG} in fire-and-forget mode"

# No deploy token: unvalidated task, no write-back, repo untouched. TASK_TIMEOUT is
# kept short so a regression (rollout wait NOT skipped) fails fast instead of
# hanging for the full default window.
if ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="60" \
   "${bin_dir}/aw-client"; then
  echo "FIRE-AND-FORGET: PASS (CronJob-only app reached 'deployed' without the image rolling out)"
  exit 0
fi
echo "FIRE-AND-FORGET: FAIL (client exited non-zero — the rollout wait was NOT skipped)"
exit 1
