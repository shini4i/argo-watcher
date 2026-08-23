#!/usr/bin/env bash
# Assert argo-watcher's read-only HTTP surface behaves to contract. Pure curl/jq,
# no deploy — runs against the live server any time after `task up`, and touches
# nothing (safe to run before the soak). Covers the endpoints the UI, client, and
# operators depend on that no other phase exercises:
#   - GET  /api/v1/version              -> 200, non-empty JSON string
#   - GET  /api/v1/config               -> 200; OIDC reported disabled, no legacy
#                                          keycloak mirror; the deploy-token secret is
#                                          redacted (json:"-");
#                                          notification targets absent but their
#                                          `enabled` flags kept — the endpoint is
#                                          unauthenticated by necessity (the UI
#                                          bootstraps its login from it)
#
# The lab runs with OIDC disabled, which is the deployment shape asserted here:
# every read stays open. Read protection under OIDC lives in the Keycloak
# integration suite (internal/server/keycloak_integration_test.go, docker-compose
# `integration` profile) until the lab grows an OIDC-enabled phase.
#   - GET  /api/v1/tasks?status=bogus   -> 400 "unsupported status filter"
#   - GET  /api/v1/tasks?status=deployed-> 200, valid JSON (filter accepted)
#   - GET  /api/v1/tasks/<unknown-uuid> -> 404 "task not found" (the 404-vs-500
#                                          distinction, commit fa0b3fd)
#   - GET  /api/v1/deploy-lock          -> 200 (read-only, always registered)
#   - POST /api/v1/tasks (>1 MiB)       -> 413; 51 images -> 406 (issue #562)
#   - POST /api/v1/deploy-lock          -> with OIDC disabled the state-changing
#                                          handler is NOT registered, so the request
#                                          falls through to the SPA static handler
#                                          (200 HTML), not a 404. Asserted
#                                          behaviourally: an unauthenticated POST
#                                          must leave the lock unset (router.go).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

# DEPLOY_TOKEN (lib.sh) is asserted ABSENT from GET /config below, proving secrets
# stay redacted.
wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "=== probe endpoints ==="
# /livez must not consult any dependency (that it ignores an unreachable state
# backend is a unit-level assertion — the base lab runs in-memory state, which
# cannot be broken from here). What the lab can prove is that both routes are
# registered and unauthenticated, and that neither reports the ArgoCD state the
# argocd-unreachable phase induces.
for path in livez readyz; do
  req GET "${AW_URL}/${path}"
  if [[ "$CODE" == "200" ]] && jq -e '.status == "up" and (has("reason") | not)' <<<"$BODY" >/dev/null 2>&1; then
    ok "/${path} -> 200 up"
  else
    bad "/${path}: code=${CODE} body=${BODY} (want 200 {\"status\":\"up\"})"
  fi
done

echo "=== version ==="
req GET "${AW_API}/version"
if [[ "$CODE" == "200" ]] && jq -e 'type == "string" and length > 0' <<<"$BODY" >/dev/null 2>&1; then
  ok "version=${BODY} (${CODE})"
else
  bad "version: code=${CODE} body=${BODY}"
fi

echo "=== config ==="
req GET "${AW_API}/config"
if [[ "$CODE" != "200" ]]; then
  bad "config: code=${CODE}"
elif ! jq -e '.oidc.enabled == false' <<<"$BODY" >/dev/null 2>&1; then
  bad "config: oidc.enabled != false (lab runs without OIDC auth)"
elif jq -e 'has("keycloak")' <<<"$BODY" >/dev/null 2>&1; then
  # The legacy keycloak mirror was removed in 1.0.0; its return would re-expose a
  # back-compat surface consumers must no longer be able to depend on.
  bad "config: legacy keycloak mirror is back"
elif grep -qF "$DEPLOY_TOKEN" <<<"$BODY"; then
  # A leaked secret here is the whole reason ServerConfig marks it json:"-".
  bad "config: deploy token leaked in /config response"
elif ! jq -e '(.webhook | has("url") | not) and (.mattermost | has("url") | not)' <<<"$BODY" >/dev/null 2>&1; then
  # /config is served unauthenticated and a webhook URL is itself the credential; the
  # lab runs with webhooks enabled, so a regression shows up here.
  bad "config: notification target exposed on an unauthenticated endpoint"
elif ! jq -e '.webhook.enabled == true' <<<"$BODY" >/dev/null 2>&1; then
  # The enabled flag must survive: downstream consumers (argo-watcher-mcp) read it.
  bad "config: webhook.enabled missing (lab runs with webhooks on)"
elif ! jq -e '.argo_cd_url | type == "string" and startswith("http") and (contains("@") | not)' <<<"$BODY" >/dev/null 2>&1; then
  # The CLI client and the Web UI build the app URL from this field, and both expect
  # a URL string — serving a url.URL object back would break them. The "@" check is
  # the other half: this endpoint is unauthenticated, so basic-auth userinfo in
  # ARGO_URL must be stripped rather than published.
  bad "config: argo_cd_url is not a userinfo-free URL string (got $(jq -c '.argo_cd_url' <<<"$BODY"))"
else
  ok "config: oidc disabled (no legacy keycloak mirror), secrets and deployment detail withheld (${CODE})"
fi

echo "=== task-list filters ==="
req GET "${AW_API}/tasks?from_timestamp=0&status=bogus"
if [[ "$CODE" == "400" ]] && jq -e '.error' <<<"$BODY" >/dev/null 2>&1; then
  ok "invalid status -> 400"
else
  bad "invalid status: code=${CODE} body=${BODY} (want 400)"
fi
req GET "${AW_API}/tasks?from_timestamp=0&status=deployed"
if [[ "$CODE" == "200" ]] && jq -e '.' <<<"$BODY" >/dev/null 2>&1; then
  ok "valid status filter -> 200"
else
  bad "valid status: code=${CODE} (want 200 + JSON)"
fi

echo "=== task not found (404 vs 500) ==="
req GET "${AW_API}/tasks/00000000-0000-0000-0000-000000000000"
if [[ "$CODE" == "404" ]] && jq -e '.error == "task not found"' <<<"$BODY" >/dev/null 2>&1; then
  ok "unknown task -> 404 task not found"
else
  bad "unknown task: code=${CODE} body=${BODY} (want 404)"
fi

echo "=== submission limits ==="
# Task submission is unauthenticated by design, so its payload is bounded (issue
# #562). Both requests are rejected before anything is stored, which keeps this
# phase side-effect free.
# Streamed from a file, not passed as an argument: a payload this size exceeds the
# kernel's per-argument limit, and curl would never run.
oversized="$(mktemp)"
trap 'rm -f "$oversized"' EXIT
{ printf '{"app":"'; head -c 1100000 /dev/zero | tr '\0' 'a'; printf '"}'; } >"$oversized"
req POST "${AW_API}/tasks" -H 'Content-Type: application/json' --data-binary @"$oversized"
if [[ "$CODE" == "413" ]]; then
  ok "oversized body -> 413"
else
  bad "oversized body: code=${CODE} body=${BODY} (want 413)"
fi

images=$(for i in $(seq 1 51); do printf '{"image":"img-%s","tag":"v1"},' "$i"; done)
post_task "{\"app\":\"limits\",\"author\":\"e2e\",\"project\":\"lab\",\"images\":[${images%,}]}"
if [[ "$CODE" == "406" ]]; then
  ok "51 images -> 406"
else
  bad "51 images: code=${CODE} body=${BODY} (want 406)"
fi

echo "=== deploy-lock endpoints ==="
req GET "${AW_API}/deploy-lock"
if [[ "$CODE" == "200" ]] && jq -e '. == false' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET deploy-lock -> 200 (unlocked)"
else
  bad "GET deploy-lock: code=${CODE} body=${BODY} (want 200 false)"
fi
# Security property: with OIDC disabled the state-changing POST/DELETE handlers
# are NOT registered (router.go), so an unauthenticated caller cannot freeze
# deploys. The unmatched route falls through to the SPA static handler (200 HTML),
# NOT a 404 — so assert the guarantee behaviourally: after an unauthenticated POST
# the lock is still not set.
req POST "${AW_API}/deploy-lock"
req GET "${AW_API}/deploy-lock"
if jq -e '. == false' <<<"$BODY" >/dev/null 2>&1; then
  ok "POST deploy-lock did not set the lock (still unlocked without OIDC)"
else
  bad "POST deploy-lock set the lock without OIDC (body=${BODY})"
fi

phase_end API-SURFACE
