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

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

# Must match the fixed UUID baked into WEBHOOK_URL (values/argo-watcher.yaml).
WEBHOOK_UUID="${WEBHOOK_UUID:-11111111-1111-1111-1111-111111111111}"
APP="${APP:-app1}"
# A pullable tag that differs from smoke's (v1.10.2) so this forces a real
# rollout to "deployed" rather than a no-op.
TAG="${TAG:-v1.10.1}"
# Must match WEBHOOK_AUTHORIZATION_HEADER_* in values/argo-watcher.yaml.
AUTH_HEADER="${AUTH_HEADER:-X-E2E-Token}"
AUTH_VALUE="${AUTH_VALUE:-e2e-webhook-secret}"

# Fail loudly if either service never answers, so an unreachable endpoint does not
# masquerade as a later "no task id" error.
wait_service || die "argo-watcher not reachable on ${AW_URL}"
wait_url "${WHT_URL}/healthz" || die "webhook-tester not reachable on ${WHT_URL}"

wht="${WHT_URL}/api/session/${WEBHOOK_UUID}"

# Start from a clean session so we assert on this deploy alone. A no-op if the
# session does not exist yet (the first webhook auto-creates it).
curl -s -m10 -X DELETE "${wht}/requests" >/dev/null 2>&1 || true

# Fire one authenticated deploy: validated -> real write-back -> "deployed".
post_task "$(task_json "$APP" "$TAG")" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}"
id=$(jq -r '.id' <<<"$BODY")
echo "task ${id}: deploying ${APP} -> ${IMAGE}:${TAG}"
[[ -n "$id" && "$id" != "null" ]] || die "no task id returned (code=${CODE} body=${BODY})"

# Wait for the terminal result so both webhooks (start + result) have fired.
status=""
terminal() {
  status=$(curl -s -m10 "${AW_API}/tasks/${id}" | jq -r '.status // "?"')
  case "$status" in deployed | failed | aborted) return 0 ;; *) return 1 ;; esac
}
retry 48 5 terminal
echo "task status=${status}"
[[ "$status" == "deployed" ]] || die "expected terminal 'deployed', got '${status}'"

# Pull captured requests for THIS task id. The capture-API shape below
# (GET .../requests, body in .request_payload_base64, headers as [{name,value}])
# matches webhook-tester v2.3.0 — the tag pinned in values/webhook-tester.yaml;
# re-verify it if that image is bumped. Body is base64 JSON; the auth header
# match is case-insensitive (Go canonicalizes header names on send/receive).
# Retry briefly — the result webhook lands right around terminal status.
events='[]'
both_captured() {
  events=$(curl -s -m10 "${wht}/requests" | jq -c --arg id "$id" --arg hdr "$AUTH_HEADER" '
    [ .[]
      | . as $r
      | (try ($r.request_payload_base64 | @base64d | fromjson) catch null) as $b
      | select($b != null and $b.id == $id)
      | { status: $b.status, app: $b.app, tag: ($b.images[0].tag // ""),
          auth: ([ $r.headers[] | select((.name|ascii_downcase) == ($hdr|ascii_downcase)) | .value ] | first // "") } ]')
  [[ "$(jq 'length' <<<"$events")" -ge 2 ]]
}
retry 15 2 both_captured
echo "captured events for task ${id}: ${events}"

count=$(jq 'length' <<<"$events")
[[ "$count" -ge 2 ]] \
  || die "expected >=2 webhook deliveries (start + result), got ${count}"
jq -e --arg a "$APP" 'any(.[]; .status == "in progress" and .app == $a)' <<<"$events" >/dev/null \
  || die "missing 'in progress' start event for app=${APP}"
jq -e --arg a "$APP" --arg t "$TAG" 'any(.[]; .status == "deployed" and .app == $a and .tag == $t)' <<<"$events" >/dev/null \
  || die "missing 'deployed' result event for app=${APP} tag=${TAG}"
jq -e --arg v "$AUTH_VALUE" 'all(.[]; .auth == $v)' <<<"$events" >/dev/null \
  || die "authorization header ${AUTH_HEADER} missing or wrong on a delivery"

echo "NOTIFICATIONS: PASS"
