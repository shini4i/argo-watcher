#!/usr/bin/env bash
# Assert argo-watcher's read-only HTTP surface behaves to contract. Pure curl/jq,
# no deploy — runs against the live server any time after `task up`, and touches
# nothing (safe to run before the soak). Covers the endpoints the UI, client, and
# operators depend on that no other phase exercises:
#   - GET  /api/v1/version              -> 200, non-empty JSON string
#   - GET  /api/v1/config               -> 200; Keycloak reported disabled; the
#                                          deploy-token secret is redacted (json:"-")
#   - GET  /api/v1/tasks?status=bogus   -> 400 "unsupported status filter"
#   - GET  /api/v1/tasks?status=deployed-> 200, valid JSON (filter accepted)
#   - GET  /api/v1/tasks/<unknown-uuid> -> 404 "task not found" (the 404-vs-500
#                                          distinction, commit fa0b3fd)
#   - GET  /api/v1/deploy-lock          -> 200 (read-only, always registered)
#   - POST /api/v1/deploy-lock          -> with Keycloak disabled the state-changing
#                                          handler is NOT registered, so the request
#                                          falls through to the SPA static handler
#                                          (200 HTML), not a 404. Asserted
#                                          behaviourally: an unauthenticated POST
#                                          must leave the lock unset (router.go).
set -euo pipefail

NS_AW="${NS_AW:-argo-watcher}"
# Must match the ARGO_WATCHER_DEPLOY_TOKEN in the argo-watcher secret; asserted
# ABSENT from GET /config to prove secrets stay redacted.
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18092}"

trap 'kill $(jobs -p) 2>/dev/null || true' EXIT
kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

base="http://localhost:${PORT}/api/v1"
fail=0

# req METHOD URL [curl-args...] -> sets CODE (HTTP status) and BODY (response body).
req() {
  local method="$1" url="$2"; shift 2
  local out
  out=$(curl -s -m 10 -w $'\n%{http_code}' -X "$method" "$@" "$url")
  CODE="${out##*$'\n'}"
  BODY="${out%$'\n'*}"
}

echo "=== version ==="
req GET "${base}/version"
if [[ "$CODE" == "200" ]] && echo "$BODY" | jq -e 'type == "string" and length > 0' >/dev/null 2>&1; then
  echo "  OK   version=${BODY} (${CODE})"
else
  echo "  FAIL version: code=${CODE} body=${BODY}"; fail=1
fi

echo "=== config ==="
req GET "${base}/config"
if [[ "$CODE" != "200" ]]; then
  echo "  FAIL config: code=${CODE}"; fail=1
elif ! echo "$BODY" | jq -e '.keycloak.enabled == false' >/dev/null 2>&1; then
  echo "  FAIL config: keycloak.enabled != false (lab runs without Keycloak)"; fail=1
elif echo "$BODY" | grep -qF "$DEPLOY_TOKEN"; then
  # A leaked secret here is the whole reason ServerConfig marks it json:"-".
  echo "  FAIL config: deploy token leaked in /config response"; fail=1
else
  echo "  OK   config: keycloak disabled, deploy token redacted (${CODE})"
fi

echo "=== task-list filters ==="
req GET "${base}/tasks?from_timestamp=0&status=bogus"
if [[ "$CODE" == "400" ]] && echo "$BODY" | jq -e '.error' >/dev/null 2>&1; then
  echo "  OK   invalid status -> 400"
else
  echo "  FAIL invalid status: code=${CODE} body=${BODY} (want 400)"; fail=1
fi
req GET "${base}/tasks?from_timestamp=0&status=deployed"
if [[ "$CODE" == "200" ]] && echo "$BODY" | jq -e '.' >/dev/null 2>&1; then
  echo "  OK   valid status filter -> 200"
else
  echo "  FAIL valid status: code=${CODE} (want 200 + JSON)"; fail=1
fi

echo "=== task not found (404 vs 500) ==="
req GET "${base}/tasks/00000000-0000-0000-0000-000000000000"
if [[ "$CODE" == "404" ]] && echo "$BODY" | jq -e '.error == "task not found"' >/dev/null 2>&1; then
  echo "  OK   unknown task -> 404 task not found"
else
  echo "  FAIL unknown task: code=${CODE} body=${BODY} (want 404)"; fail=1
fi

echo "=== deploy-lock endpoints ==="
req GET "${base}/deploy-lock"
if [[ "$CODE" == "200" ]] && echo "$BODY" | jq -e '. == false' >/dev/null 2>&1; then
  echo "  OK   GET deploy-lock -> 200 (unlocked)"
else
  echo "  FAIL GET deploy-lock: code=${CODE} body=${BODY} (want 200 false)"; fail=1
fi
# Security property: with Keycloak disabled the state-changing POST/DELETE handlers
# are NOT registered (router.go), so an unauthenticated caller cannot freeze
# deploys. The unmatched route falls through to the SPA static handler (200 HTML),
# NOT a 404 — so assert the guarantee behaviourally: after an unauthenticated POST
# the lock is still not set.
req POST "${base}/deploy-lock"
req GET "${base}/deploy-lock"
if echo "$BODY" | jq -e '. == false' >/dev/null 2>&1; then
  echo "  OK   POST deploy-lock did not set the lock (still unlocked without Keycloak)"
else
  echo "  FAIL POST deploy-lock set the lock without Keycloak (body=${BODY})"; fail=1
fi

if [[ "$fail" -eq 0 ]]; then echo "API-SURFACE: PASS"; else echo "API-SURFACE: FAIL"; exit 1; fi
