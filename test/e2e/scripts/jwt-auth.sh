#!/usr/bin/env bash
# Prove the JWT (BEARER_TOKEN) auth path end to end, distinct from the deploy-token
# path every other phase uses. argo-watcher runs with JWT_SECRET set, which
# registers the HMAC JWT strategy on the Authorization header. We mint a token with
# that same secret and deploy through the real client using BEARER_TOKEN and NO
# deploy token.
#
# The assertion is a real tag transition, not just "deployed": we deploy a tag
# DIFFERENT from the app's current one, so reaching "deployed" (client exit 0)
# requires the git write-back to have pushed the new tag — which only happens when
# the task is Validated, i.e. when the JWT was accepted. A rejected JWT leaves the
# task unvalidated, no write-back, and the client times out non-zero.
#
# Usage: jwt-auth.sh [app]
set -euo pipefail

APP="${1:-app2}"
IMAGE="${IMAGE:-traefik/whoami}"
NS_AW="${NS_AW:-argo-watcher}"
# MUST match JWT_SECRET in values/argo-watcher.yaml.
JWT_SECRET="${JWT_SECRET:-e2e-jwt-secret}"
PORT="${PORT:-18094}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

for _ in $(seq 1 40); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "JWT-AUTH: FAIL — ${APP} never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }

# Pick a tag that differs from the currently deployed one so the deploy forces a
# write-back regardless of what earlier phases left the app on. Both tags exist.
cur=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.summary.images}' 2>/dev/null || true)
if [[ "$cur" == *v1.10.2* ]]; then TAG="v1.10.1"; else TAG="v1.10.2"; fi

# Mint a short-lived HS256 JWT signed with JWT_SECRET via the in-repo Go minter
# (signs with the same library the server validates with; no openssl dependency).
jwt="$(cd "$root" && JWT_SECRET="$JWT_SECRET" go run ./test/e2e/tools/mintjwt)"

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${TAG} via BEARER_TOKEN (JWT), no deploy token"

# BEARER_TOKEN only, ARGO_WATCHER_DEPLOY_TOKEN unset: the deploy is authenticated
# solely by the JWT, so a successful write-back proves the JWT strategy validated it.
if ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   BEARER_TOKEN="${jwt}" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="180" \
   "${bin_dir}/aw-client"; then
  echo "JWT-AUTH: PASS (JWT-authenticated write-back reached 'deployed' on ${TAG})"
  exit 0
fi
echo "JWT-AUTH: FAIL (client exited non-zero — JWT likely rejected, so no write-back happened)"
exit 1
