#!/usr/bin/env bash
# Prove inline write-back through an app-of-apps: only inlineapp's marked tag in
# inline/apps.yaml changes, and the inline-apps parent's sync rolls the child out on it.
# The parent refresh after the commit stands in for the Git webhook production uses.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

PARENT="inline-apps"
APP="inlineapp"
FIXTURE="${E2E_DIR}/fixtures/inline/apps.yaml"
MARKER="# {\"\$argo-watcher\": \"${APP}:app\"}"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
client_pid=""
trap '[[ -n "$client_pid" ]] && kill "$client_pid" 2>/dev/null; rm -rf "$bin_dir" "$work"' EXIT
build_client "$bin_dir" || die "client build failed"

kubectl apply -f "${E2E_DIR}/fixtures/inline-parent-app.yaml"
require_app_synced "$PARENT" 60
require_app_synced "$APP" 60
# The other tag than the live one, so a re-run against the same lab still commits.
TAG="${TAG:-$(other_tag "$APP")}"
wait_service || die "argo-watcher never answered on ${AW_URL}"

gitops_clone "${work}/r"
tag_committed() {
  git -C "${work}/r" pull -q --ff-only >/dev/null 2>&1 \
    && grep -qF "tag: ${TAG} ${MARKER}" "${work}/r/inline/apps.yaml"
}

echo "deploying ${APP} -> ${IMAGE}:${TAG} (inline write-back)"
run_client "$APP" "$TAG" ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" >"${work}/client.log" 2>&1 &
client_pid=$!

if retry 30 2 tag_committed; then
  kubectl -n "$NS_ARGOCD" annotate application "$PARENT" \
    argocd.argoproj.io/refresh=normal --overwrite >/dev/null
else
  bad "inline/apps.yaml never carried ${TAG} on the ${APP} marker"
fi

client_rc=0
wait "$client_pid" || client_rc=$?
client_pid=""
cat "${work}/client.log"
[[ "$client_rc" -eq 0 ]] || bad "client exited ${client_rc} (deployment did not reach 'deployed')"

expected="${work}/expected.yaml"
sed "s|tag: v1.10.1 ${MARKER}|tag: ${TAG} ${MARKER}|" "$FIXTURE" >"$expected"
if ! diff -u "$expected" "${work}/r/inline/apps.yaml"; then
  bad "inline/apps.yaml differs from the fixture beyond ${APP}'s marked tag"
fi
inline_files="$(ls -A "${work}/r/inline")"
[[ "$inline_files" == "apps.yaml" ]] || bad "inline/ holds more than apps.yaml: ${inline_files//$'\n'/ }"
[[ ! -e "${work}/r/chart/.argocd-source-${APP}.yaml" ]] || bad "an override file for ${APP} landed in chart/"

live_tag=$(kubectl -n "$NS_ARGOCD" get application "$APP" \
  -o jsonpath='{.spec.source.helm.valuesObject.app.image.tag}')
[[ "$live_tag" == "$TAG" ]] || bad "live ${APP} Application carries tag ${live_tag:-<none>}, want ${TAG}"

images=$(kubectl -n "$APP" get deploy -o jsonpath='{.items[*].spec.template.spec.containers[*].image}')
[[ " ${images} " == *" ${IMAGE}:${TAG} "* ]] || bad "${APP} Deployment runs [${images}], want ${IMAGE}:${TAG}"

phase_end INLINE-WRITE-BACK
