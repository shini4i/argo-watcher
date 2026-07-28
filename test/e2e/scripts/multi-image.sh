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

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="multiapp"
# Both images publish this tag; the client applies one IMAGE_TAG to every image.
TAG="${TAG:-latest}"
IMAGES_LIST="${IMAGES_LIST:-traefik/whoami,nginxinc/nginx-unprivileged}"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
trap 'rm -rf "$bin_dir" "$work"' EXIT
build_client "$bin_dir" || die "client build failed"

# Apply the dedicated two-image fixture and wait for its initial sync (60 attempts:
# it pulls two images on first sync).
kubectl apply -f "${E2E_DIR}/fixtures/multi-image-app.yaml"
require_app_synced "$APP" 60
wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> [${IMAGES_LIST}]:${TAG} (multi-image, authenticated)"
if ! run_client "$APP" "$TAG" \
     IMAGES="$IMAGES_LIST" \
     ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN"; then
  echo "MULTI-IMAGE: FAIL (deploy did not reach 'deployed' — not all images rolled out)"
  exit 1
fi

# Read the override file written back and assert BOTH image-tag params are present.
gitops_clone "${work}/r"
override="${work}/r/multi-image/.argocd-source-${APP}.yaml"

if [[ ! -f "$override" ]]; then
  bad "override file not written: multi-image/.argocd-source-${APP}.yaml"
else
  # Assert both managed images were written back AND carry the deployed tag (not a
  # stale value) — a regression writing the wrong tag for one image would otherwise
  # slip through a names-only check.
  v_main=$(override_param "$override" app.image.tag)
  v_proxy=$(override_param "$override" app.proxyTag)
  echo "  written params: app.image.tag=${v_main:-<none>} app.proxyTag=${v_proxy:-<none>}"
  [[ "$v_main" == "$TAG" ]]  || bad "app.image.tag (primary image) not written back as ${TAG}"
  [[ "$v_proxy" == "$TAG" ]] || bad "app.proxyTag (second image) not written back as ${TAG}"
fi

if [[ "$E2E_FAILS" -eq 0 ]]; then
  echo "MULTI-IMAGE: PASS (both images deployed and both tags written back)"
  exit 0
fi
phase_end MULTI-IMAGE
