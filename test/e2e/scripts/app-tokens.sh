#!/usr/bin/env bash
# Prove application deploy tokens end to end against a REAL OIDC provider: issue one
# through the Keycloak-authenticated API, enforce its per-application scope on a real
# deploy, then revoke and expire it. See README.md for placement and rationale.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

: "${AW_CHART_REPO:?AW_CHART_REPO is required}" "${AW_CHART_VERSION:?AW_CHART_VERSION is required}"
VALUES="${E2E_DIR}/values/argo-watcher.yaml"
PG_VALUES="${E2E_DIR}/values/argo-watcher-postgres.yaml"

# The tokens live in Postgres, so every helm apply here — the revert included — must
# keep that overlay layered on the base values.
AW_EXTRA_VALUES=("$PG_VALUES")

# MUST match KC_HOSTNAME in values/keycloak.yaml: this is the issuer as the SERVER
# reaches it, while the token endpoint below is the same realm as the HOST sees it.
REALM="argo-watcher-e2e"
KC_ISSUER="http://keycloak.keycloak/realms/${REALM}"
KC_TOKEN_URL="${KEYCLOAK_URL}/realms/${REALM}/protocol/openid-connect/token"
KC_CLIENT="argo-watcher"
# A second client of the same realm, standing in for another application registered
# with the same provider (issue #577's credential binding).
KC_FOREIGN_CLIENT="other-app"
PRIV_GROUP="privileged"

# MUST match JWT_SECRET in values/argo-watcher.yaml (the prefix-routing assertion
# mints a CI JWT for the same header the tokens arrive in).
JWT_SECRET="${JWT_SECRET:-e2e-jwt-secret}"

# The one app deployed for real, and one the scoped token must NOT cover.
APP="${APP:-app4}"
DENIED_APP="app2"

# Names no Argo Application carries. An accepted task IS validated, so one naming a
# real fixture app would write its (bogus) tag back to the shared gitops repo; for
# these the task fails "app not found" in seconds, before any write-back.
SCOPE_A="scope-alpha"
SCOPE_B="scope-beta"

UNKNOWN_ID="00000000-0000-0000-0000-000000000000"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
probe_out="$(mktemp)"

revert() {
  echo "reverting OIDC_ENABLED (the Postgres overlay stays)"
  helm_apply_aw || true
  wait_service || true
  rm -rf "$bin_dir" "$work" "$probe_out"
  return
}
trap revert EXIT

# --- helpers ------------------------------------------------------------------
# kc_token <client> <user> <password>: an access token from the lab realm's direct
# access grant. The openid scope is required — Keycloak 26 answers userinfo 403
# without it, and argo-watcher validates by calling userinfo.
kc_token() {
  curl -s -m 10 -X POST "$KC_TOKEN_URL" \
    -d grant_type=password -d "client_id=$1" -d "username=$2" -d "password=$3" \
    -d scope=openid | jq -r '.access_token // empty'
  return
}

# as_priv <method> <path> [curl args...]: a request carrying the privileged
# operator's OIDC token, which is what the token endpoints demand.
as_priv() {
  local method="$1" path="$2"
  shift 2
  req "$method" "${AW_API}${path}" -H "Oidc-Authorization: Bearer ${PRIV_TOKEN}" "$@"
  return
}

# issue <json>: POST a token request as the privileged operator.
issue() {
  as_priv POST /app-tokens -H 'Content-Type: application/json' -d "$1"
  return
}

# issue_secret <json> <description>: issue a token, aborting the phase if it is
# refused, and leave its secret in TOKEN_SECRET and its id in TOKEN_ID. Values are
# returned in variables, not echoed: a die() inside $(...) would kill only the
# subshell and the caller would carry on with an empty secret.
issue_secret() {
  local json="$1" what="$2"
  issue "$json"
  [[ "$CODE" == "201" ]] || die "could not issue the ${what} token: code=${CODE} body=${BODY}"
  TOKEN_ID="$(jq -r '.id' <<<"$BODY")"
  TOKEN_SECRET="$(jq -r '.secret' <<<"$BODY")"
  return
}

# token_field <id> <jq-filter>: read one field of a token from the API listing, so
# the assertions go through the endpoint operators actually use.
token_field() {
  as_priv GET /app-tokens
  jq -r --arg id "$1" ".[] | select(.id == \$id) | $2" <<<"$BODY"
  return
}

# token_moved <id> <field>: true when the token's timestamp field carries a real
# instant. A bare `!= 0` would also pass on NO output, which is what a listing that
# is not an array prints — the failure this is most likely to face.
token_moved() {
  local value
  value="$(token_field "$1" "$2 // 0")"
  [[ "$value" =~ ^[0-9]+$ ]] && (( value > 0 ))
  return
}

build_client "$bin_dir" || die "client build failed"
build_bin "$bin_dir" ./test/e2e/tools/wsprobe || die "wsprobe build failed"
wsprobe="$BIN"

wait_service || die "argo-watcher never answered on ${AW_URL}"

# --- 0. The feature is refused by name while it is unavailable ----------------
# Without this the phase could pass against a server that accepts anything: the
# endpoints must not exist, and a token must be rejected with the reason rather
# than ignored as an unreadable header (which would skip the write-back silently).
req GET "${AW_API}/app-tokens"
if [[ "$CODE" == "200" ]] && ! jq -e 'type == "array"' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET /app-tokens is not a route with OIDC disabled (falls through to the SPA)"
else
  bad "GET /app-tokens with OIDC disabled: code=${CODE} body=$(head -c 120 <<<"$BODY")"
fi

post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: awt_unavailable"
if [[ "$CODE" == "401" ]] && jq -e '.error | test("OIDC_ENABLED and STATE_TYPE=postgres")' <<<"$BODY" >/dev/null 2>&1; then
  ok "an app token is refused by name while the feature is unavailable"
else
  bad "app token with the feature off: code=${CODE} body=${BODY} (want 401 naming the requirements)"
fi

# --- 1. Bring up Postgres + OIDC ----------------------------------------------
echo "=== provisioning Postgres and pointing the release at Keycloak ==="
kubectl apply -f "${E2E_DIR}/fixtures/postgres/" >/dev/null
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher-db --timeout=180s >/dev/null

wait_url "${KEYCLOAK_URL}/realms/${REALM}/.well-known/openid-configuration" 60 \
  || die "keycloak never answered on ${KEYCLOAK_URL}"

idx=$(extra_envs_index "$VALUES")
helm_apply_aw --set-string "extraEnvs[${idx}].name=OIDC_ENABLED" \
              --set-string "extraEnvs[${idx}].value=true" \
              --set-string "extraEnvs[$((idx + 1))].name=OIDC_ISSUER_URL" \
              --set-string "extraEnvs[$((idx + 1))].value=${KC_ISSUER}" \
              --set-string "extraEnvs[$((idx + 2))].name=OIDC_CLIENT_ID" \
              --set-string "extraEnvs[$((idx + 2))].value=${KC_CLIENT}" \
              --set-string "extraEnvs[$((idx + 3))].name=OIDC_PRIVILEGED_GROUPS" \
              --set-string "extraEnvs[$((idx + 3))].value=${PRIV_GROUP}" \
              --wait --timeout 5m
wait_service || die "argo-watcher never came back with OIDC enabled"

# Minted only now, and re-minted before the long sections below: the realm pins no
# access-token lifespan, so it inherits Keycloak's default of a few minutes — less
# than a helm rollout plus a real deploy. An expired token 401s every privileged
# call and would read as a token-management regression.
mint_operators() {
  PRIV_TOKEN="$(kc_token "$KC_CLIENT" priv-user priv-pass)"
  REGULAR_TOKEN="$(kc_token "$KC_CLIENT" regular-user regular-pass)"
  FOREIGN_TOKEN="$(kc_token "$KC_FOREIGN_CLIENT" priv-user priv-pass)"
  [[ -n "$PRIV_TOKEN" && -n "$REGULAR_TOKEN" && -n "$FOREIGN_TOKEN" ]] \
    || die "could not mint the realm's access tokens from ${KC_TOKEN_URL}"
  return
}
mint_operators

req GET "${AW_API}/config"
jq -e '.oidc.enabled == true and .state_type == "postgres"' <<<"$BODY" >/dev/null 2>&1 \
  || die "server did not come back on OIDC + Postgres (config=${BODY})"
ok "server reports oidc.enabled=true and state_type=postgres"

# --- 2. Only a privileged operator may manage tokens --------------------------
req GET "${AW_API}/app-tokens"
if [[ "$CODE" == "401" ]]; then
  ok "GET /app-tokens -> 401 without a credential"
else
  bad "GET /app-tokens uncredentialed: code=${CODE} body=${BODY} (want 401)"
fi

req GET "${AW_API}/app-tokens" -H "Oidc-Authorization: Bearer ${REGULAR_TOKEN}"
if [[ "$CODE" == "401" ]] && jq -e '.error | test("privileged groups")' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET /app-tokens -> 401 for an authenticated but unprivileged user"
else
  bad "GET /app-tokens as regular-user: code=${CODE} body=${BODY} (want 401 naming the groups)"
fi

# The list names who holds a credential for which applications, so it is as
# restricted as issuing. A token minted for another client of the same realm must
# not reach it either.
req GET "${AW_API}/app-tokens" -H "Oidc-Authorization: Bearer ${FOREIGN_TOKEN}"
if [[ "$CODE" == "401" ]] && jq -e '.error | test("not issued to")' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET /app-tokens -> 401 for a token issued to another client of the realm"
else
  bad "GET /app-tokens with a foreign-client token: code=${CODE} body=${BODY} (want 401)"
fi

as_priv GET /app-tokens
if [[ "$CODE" == "200" ]] && jq -e 'type == "array"' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET /app-tokens -> 200 for a privileged operator"
else
  bad "GET /app-tokens as priv-user: code=${CODE} body=${BODY} (want a 200 array)"
fi

# --- 3. Issuing a token --------------------------------------------------------
echo "=== issuing a token scoped to ${APP} ==="
issue "{\"apps\":[\"${APP}\"],\"description\":\"e2e ${APP} pipeline\"}"
if [[ "$CODE" != "201" ]]; then
  die "issuing failed: code=${CODE} body=${BODY}"
fi

SCOPED_ID="$(jq -r '.id' <<<"$BODY")"
SCOPED_SECRET="$(jq -r '.secret' <<<"$BODY")"
if jq -e --arg app "$APP" --arg desc "e2e ${APP} pipeline" \
     '.apps == [$app] and .all_apps == false and .created_by == "priv-user" and .description == $desc' \
     <<<"$BODY" >/dev/null 2>&1; then
  ok "the issued token records its scope, description and the operator who created it"
else
  bad "issued token metadata: $(jq -c '{apps,all_apps,created_by,description}' <<<"$BODY")"
fi

if [[ "$SCOPED_SECRET" == awt_* ]] && [[ "$(jq -r '.hint' <<<"$BODY")" == "${SCOPED_SECRET: -4}" ]]; then
  ok "the secret carries the awt_ prefix and the hint names its last four characters"
else
  bad "secret/hint mismatch: secret=${SCOPED_SECRET:0:8}... hint=$(jq -r '.hint' <<<"$BODY")"
fi

# The one response that carries a secret must not be cached by anything in front
# of the server.
if curl -s -m 10 -D - -o /dev/null -X POST "${AW_API}/app-tokens" \
     -H "Oidc-Authorization: Bearer ${PRIV_TOKEN}" -H 'Content-Type: application/json' \
     -d "{\"apps\":[\"${SCOPE_A}\"]}" | grep -qi '^cache-control: no-store'; then
  ok "the issue response is marked Cache-Control: no-store"
else
  bad "the issue response is missing Cache-Control: no-store"
fi

# The secret exists in clear exactly once: the listing can only ever show the hint.
if [[ "$(token_field "$SCOPED_ID" '.secret // "absent"')" == "absent" ]]; then
  ok "the listing never returns a token's secret"
else
  bad "the listing returned a secret for token ${SCOPED_ID}"
fi

# --- 4. A scope that matches nothing is refused --------------------------------
while read -r label json; do
  issue "$json"
  if [[ "$CODE" == "406" ]]; then
    ok "invalid scope refused (406): ${label}"
  else
    bad "invalid scope ${label}: code=${CODE} body=${BODY} (want 406)"
  fi
done <<'EOF'
neither-apps-nor-all {}
bounded-and-unbounded {"apps":["a"],"all_apps":true}
empty-application-name {"apps":["  "]}
expiry-beyond-ten-years {"apps":["a"],"expires_in_days":4000}
EOF

# The bounds on the list. Both are refused by the request validator before
# Scope.Validate runs, so these pin the API contract, not apptoken's own caps —
# those are killed by TestScopeValidate.
issue "$(jq -nc '{apps: [range(201) | "bulk-app-\(.)"]}')"
if [[ "$CODE" == "406" ]]; then
  ok "invalid scope refused (406): more applications than the cap"
else
  bad "201-application scope: code=${CODE} body=${BODY} (want 406)"
fi

issue "$(jq -nc --arg name "$(printf 'a%.0s' {1..256})" '{apps: [$name]}')"
if [[ "$CODE" == "406" ]]; then
  ok "invalid scope refused (406): an application name past 255 characters"
else
  bad "over-long application name: code=${CODE} body=${BODY} (want 406)"
fi

# Whitespace and duplicates are normalized at issue time, so the stored scope is
# the one Allows() compares against later.
issue "{\"apps\":[\"  ${SCOPE_A} \",\"${SCOPE_A}\",\"${SCOPE_B}\"]}"
PAIR_ID="$(jq -r '.id' <<<"$BODY")"
if [[ "$CODE" == "201" ]] && jq -e --arg a "$SCOPE_A" --arg b "$SCOPE_B" '.apps == [$a,$b]' <<<"$BODY" >/dev/null 2>&1; then
  ok "the scope is trimmed and deduplicated on the way in"
else
  bad "scope normalization: code=${CODE} apps=$(jq -c '.apps' <<<"$BODY")"
fi
PAIR_SECRET="$(jq -r '.secret' <<<"$BODY")"

# --- 5. The scope is what authorizes a deployment ------------------------------
echo "=== deploying ${APP} with the scoped token (BEARER_TOKEN, no deploy token) ==="
require_app_synced "$APP"
TAG="$(other_tag "$APP")"

# Reaching "deployed" on a tag the app is not on takes a write-back, which only a
# validated task gets — so the token was accepted for THIS application.
if run_client "$APP" "$TAG" BEARER_TOKEN="$SCOPED_SECRET"; then
  ok "the scoped token drove a deploy of ${APP} to 'deployed' on ${TAG}"
else
  bad "client exited non-zero — the app token was likely rejected for ${APP}"
fi

gitops_clone "${work}/repo"
got="$(override_param "${work}/repo/chart/.argocd-source-${APP}.yaml" app.image.tag)"
if [[ "$got" == "$TAG" ]]; then
  ok "the write-back committed ${APP} -> ${TAG} (the task was validated)"
else
  bad "write-back override for ${APP}: got '${got}', want '${TAG}'"
fi

# The deploy above can run for minutes; refresh before the privileged calls resume.
mint_operators

post_task "$(task_json "$DENIED_APP" v0.0.1)" -H "Authorization: ${SCOPED_SECRET}"
reason="$(jq -r '.error // empty' <<<"$BODY")"
if [[ "$CODE" == "401" ]] && [[ "$reason" == *"$DENIED_APP"* ]] && [[ "$reason" == *"$APP"* ]]; then
  ok "the same token is refused for ${DENIED_APP}, and the reason names both the app and the scope"
else
  bad "scope enforcement for ${DENIED_APP}: code=${CODE} reason=${reason} (want 401 naming app and scope)"
fi

# Application names are Kubernetes object names, so the comparison is exact: neither
# a different case nor a longer name sharing the prefix may pass.
for near in "${APP^^}" "${APP}4"; do
  post_task "$(task_json "$near" v0.0.1)" -H "Authorization: ${SCOPED_SECRET}"
  if [[ "$CODE" == "401" ]]; then
    ok "the scope does not stretch to ${near}"
  else
    bad "near-miss application ${near}: code=${CODE} body=${BODY} (want 401)"
  fi
done

# A token listing several applications covers each of them.
for app in "$SCOPE_A" "$SCOPE_B"; do
  post_task "$(task_json "$app" v0.0.1)" -H "Authorization: ${PAIR_SECRET}"
  if [[ "$CODE" == "202" ]]; then
    ok "the two-application token authorizes ${app}"
  else
    bad "two-application token for ${app}: code=${CODE} body=${BODY} (want 202)"
  fi
done
post_task "$(task_json "$DENIED_APP" v0.0.1)" -H "Authorization: ${PAIR_SECRET}"
if [[ "$CODE" == "401" ]]; then
  ok "the two-application token stops at the applications it lists"
else
  bad "two-application token for ${DENIED_APP}: code=${CODE} (want 401)"
fi
if token_moved "$PAIR_ID" .last_used_at; then
  ok "the two-application token authorized those tasks, rather than them being accepted uncredentialed"
else
  bad "the two-application token's use was never recorded, so its 202s prove nothing"
fi

# all_apps is a wildcard over applications that need not exist yet, which is why it
# is a column of its own rather than an entry in the list. The names are fictional
# on purpose: an accepted task IS validated, so one naming a real fixture app would
# write its bogus tag back to the gitops repo.
issue_secret '{"all_apps":true,"description":"e2e fleet"}' all-apps
ALL_SECRET="$TOKEN_SECRET"
ALL_ID="$TOKEN_ID"
for app in "$SCOPE_A" "$SCOPE_B" "never-registered-app"; do
  post_task "$(task_json "$app" v0.0.1)" -H "Authorization: ${ALL_SECRET}"
  if [[ "$CODE" == "202" ]]; then
    ok "the all-applications token authorizes ${app}"
  else
    bad "all-applications token for ${app}: code=${CODE} body=${BODY} (want 202)"
  fi
done
if token_moved "$ALL_ID" .last_used_at; then
  ok "the all-applications token authorized those tasks (the wildcard branch really ran)"
else
  bad "the all-applications token's use was never recorded, so its 202s prove nothing"
fi

# --- 6. Use is recorded on deployments, never on reads -------------------------
issue_secret "{\"apps\":[\"${SCOPE_A}\"]}" last-used
USED_SECRET="$TOKEN_SECRET"
USED_ID="$TOKEN_ID"
if [[ "$(token_field "$USED_ID" '.last_used_at // 0')" == "0" ]]; then
  ok "a fresh token reports no last use"
else
  bad "a freshly issued token already carries last_used_at"
fi

req GET "${AW_API}/tasks?from_timestamp=0" -H "Authorization: ${USED_SECRET}"
if [[ "$CODE" == "200" ]]; then
  ok "an app token authenticates a protected read"
else
  bad "read with an app token: code=${CODE} body=${BODY} (want 200)"
fi
if [[ "$(token_field "$USED_ID" '.last_used_at // 0')" == "0" ]]; then
  ok "the read left last_used_at unset (a polled endpoint stays read-only)"
else
  bad "a read recorded a use, turning every status poll into a write"
fi

post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: ${USED_SECRET}"
[[ "$CODE" == "202" ]] || bad "the last-used token was refused for its own app: code=${CODE}"
if token_moved "$USED_ID" .last_used_at; then
  ok "authorizing a deployment recorded the token's use"
else
  bad "last_used_at is still unset after the token authorized a deployment"
fi

# --- 7. Unknown, revoked and expired tokens ------------------------------------
post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: awt_dGhpcy1zZWNyZXQtd2FzLW5ldmVyLWlzc3VlZA"
if [[ "$CODE" == "401" ]] && jq -e '.error | test("not a known application deploy token")' <<<"$BODY" >/dev/null 2>&1; then
  ok "a well-formed token that was never issued -> 401"
else
  bad "unknown app token: code=${CODE} body=${BODY} (want 401)"
fi

echo "=== revoking ==="
req DELETE "${AW_API}/app-tokens/${SCOPED_ID}"
anon_code="$CODE"
req DELETE "${AW_API}/app-tokens/${SCOPED_ID}" -H "Oidc-Authorization: Bearer ${REGULAR_TOKEN}"
# The reason matters as much as the code here: any 401 would satisfy a code-only
# check, including one caused by a token this phase mismanaged.
if [[ "$anon_code" == "401" && "$CODE" == "401" ]] &&
   jq -e '.error | test("privileged groups")' <<<"$BODY" >/dev/null 2>&1; then
  ok "revocation is restricted to a privileged operator"
else
  bad "DELETE uncredentialed=${anon_code} unprivileged=${CODE} body=${BODY} (want 401 naming the groups)"
fi

as_priv DELETE "/app-tokens/${SCOPED_ID}"
if [[ "$CODE" == "200" ]]; then
  ok "the privileged operator revoked the token"
else
  bad "DELETE /app-tokens/${SCOPED_ID}: code=${CODE} body=${BODY} (want 200)"
fi

# The point of the store being a database read on every request: revocation takes
# effect on the next one, with nothing to restart or expire first.
post_task "$(task_json "$APP" v0.0.1)" -H "Authorization: ${SCOPED_SECRET}"
reason="$(jq -r '.error // empty' <<<"$BODY")"
if [[ "$CODE" == "401" ]] && [[ "$reason" == *revoked* ]]; then
  ok "the revoked token is refused on its very next use, and told it was revoked"
else
  bad "revoked token: code=${CODE} reason=${reason} (want 401 naming the revocation)"
fi

req GET "${AW_API}/tasks?from_timestamp=0" -H "Authorization: ${SCOPED_SECRET}"
if [[ "$CODE" == "401" ]]; then
  ok "the revoked token cannot read either"
else
  bad "read with a revoked token: code=${CODE} (want 401)"
fi

revoked_at="$(token_field "$SCOPED_ID" '.revoked_at // 0')"
if token_moved "$SCOPED_ID" .revoked_at; then
  ok "the revoked token keeps its row, stamped with when it was withdrawn"
else
  bad "the revoked token lost its audit row (revoked_at=${revoked_at})"
fi

# Revoking twice is a no-op rather than a re-stamp, and must be told apart from an
# id that never existed.
as_priv DELETE "/app-tokens/${SCOPED_ID}"
if [[ "$CODE" == "200" ]] && [[ "$(token_field "$SCOPED_ID" '.revoked_at')" == "$revoked_at" ]]; then
  ok "revoking an already-revoked token leaves the original revocation time"
else
  bad "second revocation: code=${CODE} revoked_at=$(token_field "$SCOPED_ID" '.revoked_at')"
fi

as_priv DELETE "/app-tokens/${UNKNOWN_ID}"
if [[ "$CODE" == "404" ]]; then
  ok "revoking an unknown id -> 404"
else
  bad "DELETE unknown id: code=${CODE} body=${BODY} (want 404)"
fi

as_priv DELETE "/app-tokens/not-a-uuid"
if [[ "$CODE" == "400" ]]; then
  ok "a malformed token id -> 400"
else
  bad "DELETE malformed id: code=${CODE} body=${BODY} (want 400)"
fi

echo "=== expiry ==="
issue_secret "{\"apps\":[\"${SCOPE_A}\"],\"expires_in_days\":1}" expiring
EXPIRING_SECRET="$TOKEN_SECRET"
EXPIRING_ID="$TOKEN_ID"
if token_moved "$EXPIRING_ID" .expires_at; then
  ok "expires_in_days stored an expiry"
else
  bad "expires_in_days did not store an expiry"
fi

# Waiting a day is not an option, so move the stored deadline into the past: the
# boundary Usable() applies is the row's, and this is the row.
psql_db "UPDATE app_tokens SET expires_at = now() - interval '1 hour' WHERE id = '${EXPIRING_ID}'" >/dev/null
post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: ${EXPIRING_SECRET}"
reason="$(jq -r '.error // empty' <<<"$BODY")"
if [[ "$CODE" == "401" ]] && [[ "$reason" == *expired* ]]; then
  ok "a token past its expiry is refused, and told it expired"
else
  bad "expired token: code=${CODE} reason=${reason} (want 401 naming the expiry)"
fi

# --- 8. The WebSocket and the JWT sharing the header ---------------------------
WS_URL="$AW_WS_URL" DURATION=3s "$wsprobe" >"$probe_out" 2>/dev/null || true
if grep -q '^OPEN$' "$probe_out"; then
  bad "an uncredentialed /ws handshake was accepted while OIDC is on"
else
  ok "WS handshake refused without a credential (the control for the next assertion)"
fi

WS_URL="$AW_WS_URL" DURATION=3s WSPROBE_BEARER_TOKEN="$ALL_SECRET" "$wsprobe" >"$probe_out" 2>/dev/null || true
if wait_ws_open "$probe_out"; then
  ok "WS handshake accepted with an app token"
else
  bad "WS handshake with an app token: $(tr '\n' ',' <"$probe_out") (want OPEN)"
fi

# Both credential types arrive in Authorization and are told apart by the token's
# own shape, so registering the tokens must not have displaced the JWT strategy.
jwt="$(cd "$E2E_ROOT" && JWT_SECRET="$JWT_SECRET" go run ./test/e2e/tools/mintjwt)" \
  || die "could not mint a JWT"
post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: ${jwt}"
if [[ "$CODE" == "202" ]]; then
  ok "a CI JWT still authorizes on the same header the app tokens use"
else
  bad "JWT alongside app tokens: code=${CODE} body=${BODY} (want 202)"
fi

post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: not-a-credential"
if [[ "$CODE" == "401" ]]; then
  ok "a non-prefixed value still reaches the JWT strategy and is rejected"
else
  bad "garbage on Authorization: code=${CODE} body=${BODY} (want 401)"
fi

# --- 9. Tokens outlive the process that issued them ----------------------------
# The whole reason there is no in-memory store: an issued token must survive a
# restart and be honored by every replica.
echo "=== restarting the server; the token must still authorize ==="
kubectl -n "$NS_AW" delete pod argo-watcher-0 --wait=true >/dev/null
kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s >/dev/null
wait_service || die "argo-watcher never came back after the restart"

post_task "$(task_json "$SCOPE_A" v0.0.1)" -H "Authorization: ${ALL_SECRET}"
if [[ "$CODE" == "202" ]]; then
  ok "the token still authorizes after a restart"
else
  bad "token after a restart: code=${CODE} body=${BODY} (want 202)"
fi
post_task "$(task_json "$APP" v0.0.1)" -H "Authorization: ${SCOPED_SECRET}"
reason="$(jq -r '.error // empty' <<<"$BODY")"
if [[ "$CODE" == "401" ]] && [[ "$reason" == *revoked* ]]; then
  ok "the revocation survived the restart too"
else
  bad "revoked token after a restart: code=${CODE} reason=${reason} (want 401)"
fi

# --- 10. Revert ----------------------------------------------------------------
revert
trap - EXIT

req GET "${AW_API}/app-tokens"
if [[ "$CODE" == "200" ]] && ! jq -e 'type == "array"' <<<"$BODY" >/dev/null 2>&1; then
  ok "the token endpoints are gone again with OIDC disabled"
else
  bad "after revert GET /app-tokens: code=${CODE} body=$(head -c 120 <<<"$BODY")"
fi

phase_end APP-TOKENS
