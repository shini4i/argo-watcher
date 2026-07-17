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

APP="proxyapp"
# The BARE image name the client requests; the app runs mirror.gcr.io/<this>.
IMAGE="${IMAGE:-traefik/whoami}"
# Must match the tag proxyapp runs (the shared chart's default).
TAG="${TAG:-v1.10.1}"
NS_AW="${NS_AW:-argo-watcher}"
PORT="${PORT:-18099}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the proxy-prefixed fixture and wait for its initial sync (the proxied image
# pulls through mirror.gcr.io and runs, so the app becomes Healthy).
kubectl apply -f "${here}/../fixtures/proxy-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> bare ${IMAGE}:${TAG} (app runs mirror.gcr.io/${IMAGE})"

# No deploy token: unvalidated, no write-back. TASK_TIMEOUT kept short so a
# regression (proxy match not applied) fails fast instead of hanging.
if ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="60" \
   "${bin_dir}/aw-client"; then
  echo "DOCKER-PROXY: PASS (bare image matched the proxy-prefixed running image)"
  exit 0
fi
echo "DOCKER-PROXY: FAIL (client exited non-zero — proxy-aware image match did not run)"
exit 1
