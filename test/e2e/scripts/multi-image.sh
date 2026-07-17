#!/usr/bin/env bash
# Prove a multi-image deploy: one client invocation carrying several images bumps
# ALL of them, and the rollout gate requires every one present before reporting
# "deployed". multiapp runs two DISTINCT images (traefik/whoami and
# nginxinc/nginx-unprivileged), each mapped to its own Helm image-tag value via the
# managed-images annotation.
#
# We deploy both images (IMAGES carries two entries, one shared IMAGE_TAG) with a
# deploy token, so argo-watcher writes back BOTH image-tag overrides. Assertions:
#   1. the client reaches "deployed" (exit 0) — the rollout check found both images
#   2. the write-back override file carries BOTH image-tag parameters
#
# Usage: multi-image.sh
set -euo pipefail

APP="multiapp"
# Both images publish this tag; the client applies one IMAGE_TAG to every image.
TAG="${TAG:-latest}"
IMAGES_LIST="${IMAGES_LIST:-traefik/whoami,nginxinc/nginx-unprivileged}"
NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18097}"
GITEA_PORT="${GITEA_PORT:-13003}"
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir" "$work"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

# Apply the dedicated two-image fixture and wait for its initial sync.
kubectl apply -f "${here}/../fixtures/multi-image-app.yaml"
for _ in $(seq 1 60); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "MULTI-IMAGE: FAIL — ${APP} never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> [${IMAGES_LIST}]:${TAG} (multi-image, authenticated)"
if ! ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGES_LIST}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   ARGO_WATCHER_DEPLOY_TOKEN="${DEPLOY_TOKEN}" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="180" \
   "${bin_dir}/aw-client"; then
  echo "MULTI-IMAGE: FAIL (deploy did not reach 'deployed' — not all images rolled out)"
  exit 1
fi

# Read the override file written back and assert BOTH image-tag params are present.
kubectl -n gitea port-forward svc/gitea-http "${GITEA_PORT}:3000" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${GITEA_PORT}" && break; sleep 1; done
git clone -q "http://${GITEA_ADMIN}:${GITEA_PW}@localhost:${GITEA_PORT}/e2e/gitops.git" "${work}/r"
override="${work}/r/multi-image/.argocd-source-${APP}.yaml"

fail=0
if [[ ! -f "$override" ]]; then
  echo "  FAIL override file not written: multi-image/.argocd-source-${APP}.yaml"; fail=1
else
  # Assert both managed images were written back AND carry the deployed tag (not a
  # stale value) — a regression writing the wrong tag for one image would otherwise
  # slip through a names-only check.
  v_main=$(yq -r '.helm.parameters[] | select(.name == "app.image.tag").value' "$override" 2>/dev/null || true)
  v_proxy=$(yq -r '.helm.parameters[] | select(.name == "app.proxyTag").value' "$override" 2>/dev/null || true)
  echo "  written params: app.image.tag=${v_main:-<none>} app.proxyTag=${v_proxy:-<none>}"
  [[ "$v_main" == "$TAG" ]]  || { echo "  FAIL app.image.tag (primary image) not written back as ${TAG}"; fail=1; }
  [[ "$v_proxy" == "$TAG" ]] || { echo "  FAIL app.proxyTag (second image) not written back as ${TAG}"; fail=1; }
fi

if [[ "$fail" -eq 0 ]]; then
  echo "MULTI-IMAGE: PASS (both images deployed and both tags written back)"
  exit 0
fi
echo "MULTI-IMAGE: FAIL"
exit 1
