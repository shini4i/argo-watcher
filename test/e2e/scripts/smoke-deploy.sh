#!/usr/bin/env bash
# One authenticated end-to-end deploy against a fixture app: wait for its initial
# sync, then run the REAL argo-watcher client (cmd/client) to bump the image tag
# and block until the deployment is done. Proves the whole loop — client binary ->
# task -> SSH write-back to Gitea -> Argo sync -> app Healthy on the new tag — and,
# unlike a hand-rolled curl poll, exercises the actual tool users run. The client's
# process exit code IS the assertion: 0 = "deployed", non-zero = anything else.
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
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

# Build the client once and run the binary (deterministic, no per-invocation
# `go run` compile). Built into a temp dir cleaned up on exit.
bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Wait for the app's initial sync so we deploy from a known-good baseline.
for _ in $(seq 1 40); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${TAG} via the client binary"

# The client submits the task, then polls to a terminal status on its own. A
# short RETRY_INTERVAL keeps the smoke test snappy; TASK_TIMEOUT bounds it
# server-side so a stuck sync fails instead of hanging.
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
  echo "OK: client reported the deployment as done"
  exit 0
fi
echo "FAIL: client exited non-zero (deployment did not reach 'deployed')"
exit 1
