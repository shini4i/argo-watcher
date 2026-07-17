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
#   - POST/DELETE /api/v1/deploy-lock   -> 404 when Keycloak is disabled: the
#                                          state-changing freeze switch is NOT
#                                          registered, so it cannot be reached
#                                          unauthenticated (router.go guarantee).
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

# expect_code METHOD URL WANT LABEL — status-only assertion (body irrelevant).
expect_code() {
  local method="$1" url="$2" want="$3" label="$4"
  req "$method" "$url"
  if [[ "$CODE" == "$want" ]]; then
    echo "  OK   ${label}: ${method} -> ${CODE}"
  else
    echo "  FAIL ${label}: ${method} -> ${CODE} (want ${want})"; fail=1
  fi
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
if [[ "$CODE" == "200" ]] && echo "$BODY" | jq -e 'type == "boolean"' >/dev/null 2>&1; then
  echo "  OK   GET deploy-lock -> 200 (${BODY})"
else
  echo "  FAIL GET deploy-lock: code=${CODE} body=${BODY} (want 200 boolean)"; fail=1
fi
# With Keycloak disabled these routes are deliberately NOT registered, so gin
# returns 404. A 200/401 here would mean an unauthenticated freeze switch is live.
expect_code POST "${base}/deploy-lock" 404 "POST deploy-lock (no Keycloak)"
expect_code DELETE "${base}/deploy-lock" 404 "DELETE deploy-lock (no Keycloak)"

if [[ "$fail" -eq 0 ]]; then echo "API-SURFACE: PASS"; else echo "API-SURFACE: FAIL"; exit 1; fi
