#!/usr/bin/env bash
# Prove the JWT (BEARER_TOKEN) auth path, the one every other phase does not use: a
# token minted with the lab's JWT_SECRET deploys through the real client with no
# deploy token, then the optional iss/aud binding is set on the live release for this
# phase's duration and reverted. Usage: jwt-auth.sh [app]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

: "${AW_CHART_REPO:?AW_CHART_REPO is required}" "${AW_CHART_VERSION:?AW_CHART_VERSION is required}"
VALUES="${E2E_DIR}/values/argo-watcher.yaml"

APP="${1:-app2}"
# MUST match JWT_SECRET in values/argo-watcher.yaml.
JWT_SECRET="${JWT_SECRET:-e2e-jwt-secret}"
ISSUER="https://ci.e2e.invalid"
AUDIENCE="argo-watcher-e2e"

bin_dir="$(mktemp -d)"

revert() {
  echo "reverting JWT_ISSUER / JWT_AUDIENCE"
  helm_apply_aw || true
  wait_service || true
  rm -rf "$bin_dir"
  return
}
trap revert EXIT

build_client "$bin_dir" || die "client build failed"

require_app_synced "$APP"

# A tag differing from the currently deployed one, so the deploy forces a real
# write-back regardless of what earlier phases left the app on.
TAG="$(other_tag "$APP")"

# Minted by the in-repo Go tool, which signs with the very library the server
# validates with (no openssl dependency).
mint() { (cd "$E2E_ROOT" && env JWT_SECRET="$JWT_SECRET" "$@" go run ./test/e2e/tools/mintjwt); }
plain_jwt="$(mint)"
bound_jwt="$(mint JWT_ISS="$ISSUER" JWT_AUD="$AUDIENCE")"

wait_service || die "argo-watcher never answered on ${AW_URL}"

# --- 1. A claimless token authenticates while nothing is configured -----------
echo "deploying ${APP} -> ${IMAGE}:${TAG} via BEARER_TOKEN (JWT), no deploy token"

# BEARER_TOKEN only, no deploy token: reaching "deployed" on a tag the app is not on
# takes a write-back, which only a Validated task gets — so the JWT was accepted. A
# rejected one leaves the task unvalidated and the client times out non-zero.
if run_client "$APP" "$TAG" BEARER_TOKEN="$plain_jwt"; then
  ok "JWT-authenticated write-back reached 'deployed' on ${TAG}"
else
  bad "client exited non-zero — JWT likely rejected, so no write-back happened"
fi

# --- 2. Configure the claim binding -------------------------------------------
idx=$(extra_envs_index "$VALUES")
helm_apply_aw --set-string "extraEnvs[${idx}].name=JWT_ISSUER" \
              --set-string "extraEnvs[${idx}].value=${ISSUER}" \
              --set-string "extraEnvs[$((idx + 1))].name=JWT_AUDIENCE" \
              --set-string "extraEnvs[$((idx + 1))].value=${AUDIENCE}"
wait_service || die "argo-watcher never came back after setting the claim binding"

# The same token section 1 deployed with. Strict means strict: a missing claim is a
# rejection, which is why the rollout order is mint-first, configure-second.
# Every POST below reuses TAG, which section 1 already rolled out, so an accepted task
# finishes on its first poll and no watch is left running across the helm upgrades.
post_task "$(task_json "$APP" "$TAG")" -H "Authorization: ${plain_jwt}"
if [[ "$CODE" == "401" ]]; then
  ok "a token without iss/aud -> 401 once the binding is configured"
else
  bad "token without iss/aud: code=${CODE} body=${BODY} (want 401)"
fi

post_task "$(task_json "$APP" "$TAG")" -H "Authorization: ${bound_jwt}"
if [[ "$CODE" == "202" ]]; then
  ok "a token carrying the configured iss/aud is accepted"
else
  bad "token with iss/aud: code=${CODE} body=${BODY} (want 202)"
fi

# The point of the setting: one HMAC secret shared across a CI estate no longer
# authorizes another system's tokens here.
foreign_jwt="$(mint JWT_ISS="https://elsewhere.invalid" JWT_AUD="$AUDIENCE")"
post_task "$(task_json "$APP" "$TAG")" -H "Authorization: ${foreign_jwt}"
if [[ "$CODE" == "401" ]]; then
  ok "a token from another issuer -> 401"
else
  bad "token from another issuer: code=${CODE} body=${BODY} (want 401)"
fi

# --- 3. Revert and prove the binding is the only thing that changed -----------
revert
trap - EXIT

post_task "$(task_json "$APP" "$TAG")" -H "Authorization: ${plain_jwt}"
if [[ "$CODE" == "202" ]]; then
  ok "the claimless token is accepted again once the binding is unset"
else
  bad "after revert, token without iss/aud: code=${CODE} body=${BODY} (want 202)"
fi

# --- 4. allowed_apps confines a token to the applications it names -------------
# The other half of the claim policy, and the JWT counterpart of an application
# deploy token's scope. The accepted names are fictional on purpose: the task IS
# validated, so a real fixture app would take its bogus tag through a write-back.
SCOPED_A="jwt-scope-alpha"
SCOPED_B="jwt-scope-beta"
scoped_jwt="$(mint JWT_ALLOWED_APPS="${SCOPED_A},${SCOPED_B}")"

for app in "$SCOPED_A" "$SCOPED_B"; do
  post_task "$(task_json "$app" v0.0.1)" -H "Authorization: ${scoped_jwt}"
  if [[ "$CODE" == "202" ]]; then
    ok "allowed_apps authorizes ${app}"
  else
    bad "allowed_apps token for ${app}: code=${CODE} body=${BODY} (want 202)"
  fi
done

post_task "$(task_json "$APP" "$TAG")" -H "Authorization: ${scoped_jwt}"
if [[ "$CODE" == "401" ]] && jq -e '.error | test("allowed_apps")' <<<"$BODY" >/dev/null 2>&1; then
  ok "allowed_apps refuses ${APP}, naming the claim that confined the token"
else
  bad "allowed_apps token for ${APP}: code=${CODE} body=${BODY} (want 401 naming the claim)"
fi

# Section 1 already deployed with a claimless token; this states the compatibility
# rule outright, since a fleet that never set the claim depends on it.
post_task "$(task_json "$APP" "$TAG")" -H "Authorization: ${plain_jwt}"
if [[ "$CODE" == "202" ]]; then
  ok "a token omitting allowed_apps still authorizes any application"
else
  bad "claimless token after the scoped one: code=${CODE} (want 202)"
fi

phase_end JWT-AUTH
