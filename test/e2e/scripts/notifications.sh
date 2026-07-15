#!/usr/bin/env bash
# Assert argo-watcher's generic webhook notifications fire correctly against a
# REAL in-cluster receiver (tarampampam/webhook-tester). One authenticated
# deploy must produce TWO deliveries — the "in progress" start event and the
# terminal "deployed" result — each carrying the templated JSON body and the
# configured authorization header.
#
# Runs on a clean state (right after smoke, before load/race) and asserts only
# on THIS deploy's task id, so unrelated soak traffic to the shared receiver
# does not affect the result. The receiver captures every request and serves it
# back over GET /api/session/<uuid>/requests (body base64-encoded).
#
# Usage: DEPLOY_TOKEN=... WEBHOOK_UUID=... notifications.sh
set -uo pipefail

NS_AW="${NS_AW:-argo-watcher}"
NS_WHT="${NS_WHT:-webhook-tester}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
# Must match the fixed UUID baked into WEBHOOK_URL (values/argo-watcher.yaml).
WEBHOOK_UUID="${WEBHOOK_UUID:-11111111-1111-1111-1111-111111111111}"
APP="${APP:-app1}"
IMAGE="${IMAGE:-traefik/whoami}"
# A pullable tag that differs from smoke's (v1.10.2) so this forces a real
# rollout to "deployed" rather than a no-op.
TAG="${TAG:-v1.10.1}"
# Must match WEBHOOK_AUTHORIZATION_HEADER_* in values/argo-watcher.yaml.
AUTH_HEADER="${AUTH_HEADER:-X-E2E-Token}"
AUTH_VALUE="${AUTH_VALUE:-e2e-webhook-secret}"
AW_PORT="${AW_PORT:-18096}"
WHT_PORT="${WHT_PORT:-18097}"

kubectl -n "$NS_AW"  port-forward svc/argo-watcher   "${AW_PORT}:80"  >/dev/null 2>&1 &
kubectl -n "$NS_WHT" port-forward svc/webhook-tester "${WHT_PORT}:80" >/dev/null 2>&1 &
trap 'kill $(jobs -p) 2>/dev/null || true' EXIT
for _ in $(seq 1 15); do curl -s -m3 -o /dev/null "localhost:${AW_PORT}/healthz"  && break; sleep 1; done
for _ in $(seq 1 15); do curl -s -m3 -o /dev/null "localhost:${WHT_PORT}/healthz" && break; sleep 1; done

wht="localhost:${WHT_PORT}/api/session/${WEBHOOK_UUID}"

# Start from a clean session so we assert on this deploy alone. A no-op if the
# session does not exist yet (the first webhook auto-creates it).
curl -s -m10 -X DELETE "${wht}/requests" >/dev/null 2>&1 || true

# Fire one authenticated deploy: validated -> real write-back -> "deployed".
id=$(curl -s -m15 -X POST "localhost:${AW_PORT}/api/v1/tasks" \
  -H 'Content-Type: application/json' -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}" \
  -d "{\"app\":\"${APP}\",\"author\":\"e2e\",\"project\":\"lab\",\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${TAG}\"}]}" \
  | jq -r '.id')
echo "task ${id}: deploying ${APP} -> ${IMAGE}:${TAG}"
[[ -n "$id" && "$id" != "null" ]] || { echo "FAIL: no task id returned"; exit 1; }

# Wait for the terminal result so both webhooks (start + result) have fired.
status=""
for _ in $(seq 1 48); do
  status=$(curl -s -m10 "localhost:${AW_PORT}/api/v1/tasks/${id}" | jq -r '.status // "?"')
  case "$status" in deployed|failed|aborted) break ;; esac
  sleep 5
done
echo "task status=${status}"
[[ "$status" == "deployed" ]] || { echo "FAIL: expected terminal 'deployed', got '${status}'"; exit 1; }

# Pull captured requests for THIS task id. The capture-API shape below
# (GET .../requests, body in .request_payload_base64, headers as [{name,value}])
# matches webhook-tester v2.3.0 — the tag pinned in values/webhook-tester.yaml;
# re-verify it if that image is bumped. Body is base64 JSON; the auth header
# match is case-insensitive (Go canonicalizes header names on send/receive).
# Retry briefly — the result webhook lands right around terminal status.
events='[]'
for _ in $(seq 1 15); do
  events=$(curl -s -m10 "${wht}/requests" | jq -c --arg id "$id" --arg hdr "$AUTH_HEADER" '
    [ .[]
      | . as $r
      | (try ($r.request_payload_base64 | @base64d | fromjson) catch null) as $b
      | select($b != null and $b.id == $id)
      | { status: $b.status, app: $b.app, tag: ($b.images[0].tag // ""),
          auth: ([ $r.headers[] | select((.name|ascii_downcase) == ($hdr|ascii_downcase)) | .value ] | first // "") } ]')
  [[ "$(jq 'length' <<<"$events")" -ge 2 ]] && break
  sleep 2
done
echo "captured events for task ${id}: ${events}"

fail() { echo "FAIL: $1"; exit 1; }
count=$(jq 'length' <<<"$events")
[[ "$count" -ge 2 ]] || fail "expected >=2 webhook deliveries (start + result), got ${count}"
jq -e --arg a "$APP" 'any(.[]; .status == "in progress" and .app == $a)' <<<"$events" >/dev/null \
  || fail "missing 'in progress' start event for app=${APP}"
jq -e --arg a "$APP" --arg t "$TAG" 'any(.[]; .status == "deployed" and .app == $a and .tag == $t)' <<<"$events" >/dev/null \
  || fail "missing 'deployed' result event for app=${APP} tag=${TAG}"
jq -e --arg v "$AUTH_VALUE" 'all(.[]; .auth == $v)' <<<"$events" >/dev/null \
  || fail "authorization header ${AUTH_HEADER} missing or wrong on a delivery"

echo "NOTIFICATIONS: PASS"
