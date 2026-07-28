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

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="${1:-app1}"
TAG="${2:-v1.10.2}"

# Build the client once and run the binary (deterministic, no per-invocation
# `go run` compile). Built into a temp dir cleaned up on exit.
bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
build_client "$bin_dir" || die "client build failed"

# Wait for the app's initial sync so we deploy from a known-good baseline.
require_app_synced "$APP"
wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> ${IMAGE}:${TAG} via the client binary"

# The client submits the task, then polls to a terminal status on its own. The
# short RETRY_INTERVAL and bounded TASK_TIMEOUT (run_client defaults) keep the
# smoke test snappy and make a stuck sync fail instead of hang. The deploy token
# enables write-back — an unauthenticated task is accepted but skips it
# (Validated=false).
if run_client "$APP" "$TAG" ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN"; then
  echo "OK: client reported the deployment as done"
  exit 0
fi
echo "FAIL: client exited non-zero (deployment did not reach 'deployed')"
exit 1
