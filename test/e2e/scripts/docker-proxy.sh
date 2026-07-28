#!/usr/bin/env bash
# Prove DOCKER_IMAGES_PROXY: argo-watcher matches a deploy's bare image name
# against the app's actual running image even when the latter carries a registry
# proxy prefix. proxyapp runs mirror.gcr.io/traefik/whoami (the shared chart with
# the repository overridden); argo-watcher runs with DOCKER_IMAGES_PROXY=mirror.gcr.io.
#
# We deploy the BARE image name (traefik/whoami) at the tag the app already runs,
# with no token (no write-back). The rollout image check finds the app's
# proxy-prefixed image only via the proxy form, so the deploy reaches "deployed"
# (exit 0). Without DOCKER_IMAGES_PROXY the bare name would never match the
# prefixed image and the deploy would time out. Exit 0 is the discriminating
# evidence that the proxy-aware match ran.
#
# Usage: docker-proxy.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="proxyapp"
# IMAGE (lib.sh) is the BARE image name the client requests; the app runs
# mirror.gcr.io/<it>.
# Must match the tag proxyapp runs (the shared chart's default).
TAG="${TAG:-v1.10.1}"

bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
build_client "$bin_dir" || die "client build failed"

# Apply the proxy-prefixed fixture and wait for its initial sync (the proxied image
# pulls through mirror.gcr.io and runs, so the app becomes Healthy).
kubectl apply -f "${E2E_DIR}/fixtures/proxy-app.yaml"
require_app_synced "$APP" 60
wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> bare ${IMAGE}:${TAG} (app runs mirror.gcr.io/${IMAGE})"

# No deploy token: unvalidated, no write-back. TASK_TIMEOUT kept short so a
# regression (proxy match not applied) fails fast instead of hanging.
if run_client "$APP" "$TAG" TASK_TIMEOUT="60"; then
  echo "DOCKER-PROXY: PASS (bare image matched the proxy-prefixed running image)"
  exit 0
fi
echo "DOCKER-PROXY: FAIL (client exited non-zero — proxy-aware image match did not run)"
exit 1
