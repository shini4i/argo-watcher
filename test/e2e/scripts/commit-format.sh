#!/usr/bin/env bash
# Prove COMMIT_MESSAGE_FORMAT: argo-watcher renders a user-supplied Go template
# (against the deploy task) into the git write-back commit message. argo-watcher
# runs with COMMIT_MESSAGE_FORMAT="e2e-commit-fmt app={{.App}} by={{.Author}}"; we
# drive an authenticated deploy that forces a real write-back, then read the commit
# it produced from the gitops repo and assert the rendered message.
#
# The malformed-template fallback (a bad template must not abort the deploy, it
# falls back to the default message) needs a DIFFERENT server env than the valid
# format asserted here, so it cannot be exercised in the same lab run; it stays
# covered at the unit level (internal/updater).
#
# Usage: commit-format.sh
set -euo pipefail

APP="app3"
IMAGE="${IMAGE:-traefik/whoami}"
AUTHOR="commitfmt-tester"
NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18096}"
GITEA_PORT="${GITEA_PORT:-13002}"
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
# Must match COMMIT_MESSAGE_FORMAT in values/argo-watcher.yaml, rendered for this
# deploy (App=app3, Author=commitfmt-tester).
WANT_MSG="e2e-commit-fmt app=${APP} by=${AUTHOR}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir" "$work"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

for _ in $(seq 1 40); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "COMMIT-FORMAT: FAIL — ${APP} never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }

# Deploy a tag differing from the current one so the write-back actually commits
# (an unchanged tag is byte-compared and skipped).
cur=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.summary.images}' 2>/dev/null || true)
if [[ "$cur" == *v1.10.2* ]]; then TAG="v1.10.1"; else TAG="v1.10.2"; fi

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${TAG} (author=${AUTHOR}) to force a write-back commit"
if ! ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="${AUTHOR}" \
   PROJECT_NAME="lab" \
   ARGO_WATCHER_DEPLOY_TOKEN="${DEPLOY_TOKEN}" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="180" \
   "${bin_dir}/aw-client"; then
  echo "COMMIT-FORMAT: FAIL (deploy did not reach 'deployed', no commit to inspect)"
  exit 1
fi

# Read the subject of the last commit touching this app's override file.
kubectl -n gitea port-forward svc/gitea-http "${GITEA_PORT}:3000" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${GITEA_PORT}" && break; sleep 1; done
git clone -q "http://${GITEA_ADMIN}:${GITEA_PW}@localhost:${GITEA_PORT}/e2e/gitops.git" "${work}/r"
got=$(cd "${work}/r" && git log -1 --format=%s -- "chart/.argocd-source-${APP}.yaml")

echo "  want: ${WANT_MSG}"
echo "  got:  ${got}"
if [[ "$got" == "$WANT_MSG" ]]; then
  echo "COMMIT-FORMAT: PASS (write-back commit carries the rendered template)"
  exit 0
fi
echo "COMMIT-FORMAT: FAIL (commit message did not match the rendered template)"
exit 1
