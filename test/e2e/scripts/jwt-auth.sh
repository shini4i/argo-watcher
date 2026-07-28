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

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="${1:-app2}"
# MUST match JWT_SECRET in values/argo-watcher.yaml.
JWT_SECRET="${JWT_SECRET:-e2e-jwt-secret}"

bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
build_client "$bin_dir" || die "client build failed"

require_app_synced "$APP"

# A tag differing from the currently deployed one, so the deploy forces a real
# write-back regardless of what earlier phases left the app on.
TAG="$(other_tag "$APP")"

# Mint a short-lived HS256 JWT signed with JWT_SECRET via the in-repo Go minter
# (signs with the same library the server validates with; no openssl dependency).
jwt="$(cd "$E2E_ROOT" && JWT_SECRET="$JWT_SECRET" go run ./test/e2e/tools/mintjwt)"

wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> ${IMAGE}:${TAG} via BEARER_TOKEN (JWT), no deploy token"

# BEARER_TOKEN only, ARGO_WATCHER_DEPLOY_TOKEN unset: the deploy is authenticated
# solely by the JWT, so a successful write-back proves the JWT strategy validated it.
if run_client "$APP" "$TAG" BEARER_TOKEN="$jwt"; then
  echo "JWT-AUTH: PASS (JWT-authenticated write-back reached 'deployed' on ${TAG})"
  exit 0
fi
echo "JWT-AUTH: FAIL (client exited non-zero — JWT likely rejected, so no write-back happened)"
exit 1
