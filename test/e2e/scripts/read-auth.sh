#!/usr/bin/env bash
# Assert the OIDC read-protection contract against the real server.
#
# OIDC_ENABLED is server-global, so — like lockdown.sh — this phase toggles it on the
# live release for its own duration and reverts before exiting.
#
# It runs WITHOUT an identity provider: the issuer points at a closed port, so any token
# the server must check fails with a transport error. That covers the contract's
# interesting half with no Keycloak in the lab:
#
#   - no credential        -> 401 (answered before any provider call)
#   - an OIDC token        -> 503 (the provider could not be consulted)
#   - a deploy token / JWT -> 200 (validated locally)
#
# The last section also flips OIDC_REQUIRE_TASK_READ_AUTH, which closes the task
# lookup left open for clients, and drives the real client against it both with and
# without a credential.
#
# The 503 case is asserted repeatedly, not once: the lab sets JWT_SECRET, so a request
# carrying both headers has one strategy rejecting and one reporting the outage, and
# strategy order is a randomized map iteration.
#
# Group membership is not observable with the provider unreachable; that is covered
# against a real Keycloak in internal/server/keycloak_integration_test.go.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

: "${AW_CHART_REPO:?AW_CHART_REPO is required}" "${AW_CHART_VERSION:?AW_CHART_VERSION is required}"
VALUES="${E2E_DIR}/values/argo-watcher.yaml"
# MUST match JWT_SECRET in values/argo-watcher.yaml — the JWT read below is signed
# with it and the server verifies against the value it was deployed with.
JWT_SECRET="${JWT_SECRET:-e2e-jwt-secret}"

# A closed port on the pod's own loopback: connection refused is immediate, so each
# assertion costs nothing while still exercising the provider-unreachable path.
UNREACHABLE_ISSUER="http://127.0.0.1:1"
UNKNOWN_TASK="00000000-0000-0000-0000-000000000000"
OIDC_HEADER="Oidc-Authorization: Bearer e2e-oidc-token"
# Structurally invalid as an HMAC JWT, so the JWT strategy on Authorization rejects
# it — the second credential in the mixed-outcome case below.
BAD_JWT_HEADER="Authorization: Bearer not-a-real-jwt"

# The reads that must require a credential once OIDC is on. Kept as a list so a new
# protected read is one line here.
PROTECTED=(
  "tasks?from_timestamp=0"
  "version"
  "reachability"
  "deploy-lock"
)

bin_dir="$(mktemp -d)"
probe_out="$(mktemp)"

revert() {
  echo "reverting OIDC_ENABLED"
  helm_apply_aw || true
  wait_service || true
  rm -rf "$bin_dir" "$probe_out"
  return
}
trap revert EXIT

build_bin "$bin_dir" ./test/e2e/tools/wsprobe || die "wsprobe build failed"
wsprobe="$BIN"

wait_service || die "argo-watcher never answered on ${AW_URL}"

# --- 0. Baseline: with OIDC off the same reads are open ------------------------
# Without this the phase could pass against a server that rejects reads for some
# unrelated reason.
for path in "${PROTECTED[@]}"; do
  req GET "${AW_API}/${path}"
  [[ "$CODE" == "200" ]] || bad "baseline (OIDC off) GET /${path}: code=${CODE}, want 200"
done
ok "baseline: every read is open with OIDC disabled"

# --- 1. Enable OIDC against an unreachable issuer -----------------------------
# ClientId is mandatory when enabled (validateServerConfig), so the pod would
# CrashLoop without it. PrivilegedGroups is optional and irrelevant here.
idx=$(extra_envs_index "$VALUES")
helm_apply_aw --set-string "extraEnvs[${idx}].name=OIDC_ENABLED" \
              --set-string "extraEnvs[${idx}].value=true" \
              --set-string "extraEnvs[$((idx + 1))].name=OIDC_ISSUER_URL" \
              --set-string "extraEnvs[$((idx + 1))].value=${UNREACHABLE_ISSUER}" \
              --set-string "extraEnvs[$((idx + 2))].name=OIDC_CLIENT_ID" \
              --set-string "extraEnvs[$((idx + 2))].value=argo-watcher-e2e"
wait_service || die "argo-watcher never came back after enabling OIDC"

req GET "${AW_API}/config"
if jq -e '.oidc.enabled == true' <<<"$BODY" >/dev/null 2>&1; then
  ok "server reports oidc.enabled=true"
else
  die "server did not come back with OIDC enabled (config=${BODY})"
fi

# --- 2. Protected reads reject an uncredentialed caller -----------------------
for path in "${PROTECTED[@]}"; do
  req GET "${AW_API}/${path}"
  if [[ "$CODE" == "401" ]] && jq -e '.error | test("authentication required")' <<<"$BODY" >/dev/null 2>&1; then
    ok "GET /${path} -> 401 without a credential"
  else
    bad "GET /${path}: code=${CODE} body=${BODY} (want 401 authentication required)"
  fi
done

# --- 3. An unreachable provider is 503, never 401 -----------------------------
# 401 is what makes the Web UI drop its session, so this is the distinction the
# whole error mapping exists for.
req GET "${AW_API}/tasks?from_timestamp=0" -H "$OIDC_HEADER"
if [[ "$CODE" == "503" ]]; then
  ok "GET /tasks with an OIDC token -> 503 (provider unreachable)"
else
  bad "GET /tasks with an OIDC token: code=${CODE} body=${BODY} (want 503)"
fi

# Mixed outcomes across strategies: the provider-unavailable signal must win over the
# JWT strategy's rejection on every request, not on average.
mixed_fails=0
for _ in $(seq 1 15); do
  req GET "${AW_API}/tasks?from_timestamp=0" -H "$OIDC_HEADER" -H "$BAD_JWT_HEADER"
  [[ "$CODE" == "503" ]] || mixed_fails=$((mixed_fails + 1))
done
if [[ "$mixed_fails" == "0" ]]; then
  ok "OIDC token + rejected JWT -> 503 on all 15 attempts (precedence is stable)"
else
  bad "OIDC token + rejected JWT: ${mixed_fails}/15 attempts were not 503 (strategy precedence depends on map order)"
fi

# --- 4. Machine credentials read without touching the provider ----------------
req GET "${AW_API}/tasks?from_timestamp=0" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}"
if [[ "$CODE" == "200" ]]; then
  ok "GET /tasks with the deploy token -> 200"
else
  bad "GET /tasks with the deploy token: code=${CODE} body=${BODY} (want 200)"
fi

jwt="$(cd "$E2E_ROOT" && JWT_SECRET="$JWT_SECRET" go run ./test/e2e/tools/mintjwt)" ||
  die "could not mint a JWT"
req GET "${AW_API}/tasks?from_timestamp=0" -H "Authorization: ${jwt}"
if [[ "$CODE" == "200" ]]; then
  ok "GET /tasks with a valid JWT -> 200 (a pipeline can read)"
else
  bad "GET /tasks with a valid JWT: code=${CODE} body=${BODY} (want 200)"
fi

# --- 5. The deliberate exemptions stay reachable -------------------------------
# Breaking any of these breaks either every pipeline (the task lookup), the Web UI's
# ability to log in at all (/config), or the probes and the scrape.
req GET "${AW_API}/tasks/${UNKNOWN_TASK}"
if [[ "$CODE" == "404" ]] && jq -e '.error == "task not found"' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET /tasks/<id> -> 404 without a credential (reached the handler)"
else
  bad "GET /tasks/<id>: code=${CODE} body=${BODY} (want 404 task not found)"
fi

req GET "${AW_API}/config"
if [[ "$CODE" == "200" ]] &&
   jq -e '(.webhook | has("url") | not) and (.mattermost | has("url") | not)' <<<"$BODY" >/dev/null 2>&1; then
  ok "GET /config -> 200 without a credential, notification targets withheld"
else
  bad "GET /config: code=${CODE} (want 200 with no notification targets)"
fi

req GET "${AW_URL}/healthz"
if [[ "$CODE" == "200" ]]; then
  ok "GET /healthz -> 200 (probes unaffected)"
else
  bad "GET /healthz: code=${CODE} (want 200)"
fi

req GET "${AW_URL}/metrics"
if [[ "$CODE" == "200" ]]; then
  ok "GET /metrics -> 200 (scrape unaffected)"
else
  bad "GET /metrics: code=${CODE} (want 200)"
fi

# A malformed body proves the request reached addTask: 406 comes from payload
# parsing, 401 would come from the middleware. Moving POST /tasks behind auth would
# reject every uncredentialed pipeline in the fleet at once.
req POST "${AW_API}/tasks" -H 'Content-Type: application/json' -d '{'
if [[ "$CODE" == "406" ]]; then
  ok "POST /tasks -> 406 without a credential (submission is not gated)"
else
  bad "POST /tasks: code=${CODE} body=${BODY} (want 406 — 401 means submission got gated)"
fi

# --- 6. The migration counter tracks credential-less reads only ---------------
before="$(metric_sum unauthenticated_reads)"
req GET "${AW_API}/tasks/${UNKNOWN_TASK}"
after_open="$(metric_sum unauthenticated_reads)"
if (( after_open > before )); then
  ok "unauthenticated_reads counted the credential-less lookup (${before} -> ${after_open})"
else
  bad "unauthenticated_reads did not count a credential-less lookup (${before} -> ${after_open})"
fi

if curl -s -m 10 "${AW_URL}/metrics" | grep -q 'unauthenticated_reads{app="unknown",path="/api/v1/tasks/:id"}'; then
  ok "the counter is labelled with the route pattern, app unknown for a lookup that matched no task"
else
  bad "no unauthenticated_reads series labelled app=\"unknown\",path=\"/api/v1/tasks/:id\""
fi

# A credentialed read must not inflate the number whose fall to zero is what
# licenses closing this endpoint.
req GET "${AW_API}/tasks/${UNKNOWN_TASK}" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}"
after_creds="$(metric_sum unauthenticated_reads)"
if [[ "$after_creds" == "$after_open" ]]; then
  ok "a credentialed lookup left the counter unchanged (${after_creds})"
else
  bad "a credentialed lookup moved the counter (${after_open} -> ${after_creds})"
fi

# The label that makes the counter actionable: an operator has to know WHICH pipeline
# still polls without a credential, so a lookup that resolves to a task must carry that
# task's app rather than a bare total. The app does not exist in Argo CD — the task is
# never deployed, and the lookup only has to resolve.
labelled_app="read-auth-metric-label"
task_id="$(curl -s -m 10 -X POST "${AW_API}/tasks" -H 'Content-Type: application/json' \
  -d "{\"app\":\"${labelled_app}\",\"author\":\"e2e\",\"project\":\"lab\",\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.1\"}]}" |
  jq -r '.id // empty')"
if [[ -z "$task_id" ]]; then
  bad "could not create a task to assert the app label"
else
  req GET "${AW_API}/tasks/${task_id}"
  if curl -s -m 10 "${AW_URL}/metrics" | grep -q "unauthenticated_reads{app=\"${labelled_app}\",path=\"/api/v1/tasks/:id\"}"; then
    ok "the counter names the app behind the uncredentialed lookup"
  else
    bad "no unauthenticated_reads series labelled app=\"${labelled_app}\""
  fi
fi

# --- 7. The WebSocket handshake is gated too -----------------------------------
# /ws broadcasts the same deploy-lock and reachability transitions the REST reads now
# require a credential for, so leaving it open would make that gating cosmetic.
WS_URL="$AW_WS_URL" DURATION=5s "$wsprobe" >"$probe_out" 2>/dev/null || true
if grep -q "^REJECTED status=401$" "$probe_out"; then
  ok "WS handshake -> 401 without a credential"
else
  bad "WS handshake without a credential: $(tr '\n' ',' <"$probe_out") (want REJECTED status=401)"
fi

# The deploy token stands in for any non-browser client; the provider is never consulted
# for it, so this is the one credential that can establish a socket in this phase.
WS_URL="$AW_WS_URL" DURATION=2s WSPROBE_DEPLOY_TOKEN="$DEPLOY_TOKEN" "$wsprobe" >"$probe_out" 2>/dev/null || true
if wait_ws_open "$probe_out"; then
  ok "WS handshake accepted with a deploy token"
else
  bad "WS handshake with a deploy token: $(tr '\n' ',' <"$probe_out") (want OPEN)"
fi

# The subprotocol transport a browser is limited to: reaching the OIDC strategy at all
# proves the token was extracted, and the unreachable provider makes it a 503.
WS_URL="$AW_WS_URL" DURATION=5s WSPROBE_SUBPROTOCOL_TOKEN=browser-token "$wsprobe" >"$probe_out" 2>/dev/null || true
if grep -q "^REJECTED status=503$" "$probe_out"; then
  ok "WS subprotocol token reaches the provider (503 while it is unreachable)"
else
  bad "WS subprotocol handshake: $(tr '\n' ',' <"$probe_out") (want REJECTED status=503)"
fi

# --- 8. The privileged write path is registered but cannot be authorized ------
req POST "${AW_API}/deploy-lock" -H "$OIDC_HEADER"
if [[ "$CODE" == "503" ]]; then
  ok "POST /deploy-lock -> 503 (registered under OIDC, provider unreachable)"
else
  bad "POST /deploy-lock: code=${CODE} body=${BODY} (want 503)"
fi

# --- 9. OIDC_REQUIRE_TASK_READ_AUTH closes the task lookup too -----------------
# The exemption in section 5 is a migration allowance. This is the end of it: with
# the variable set, the lookup is gated like every other read, and the client — which
# presents its credential on reads — keeps polling through it.
idx=$(extra_envs_index "$VALUES")
helm_apply_aw --set-string "extraEnvs[${idx}].name=OIDC_ENABLED" \
              --set-string "extraEnvs[${idx}].value=true" \
              --set-string "extraEnvs[$((idx + 1))].name=OIDC_ISSUER_URL" \
              --set-string "extraEnvs[$((idx + 1))].value=${UNREACHABLE_ISSUER}" \
              --set-string "extraEnvs[$((idx + 2))].name=OIDC_CLIENT_ID" \
              --set-string "extraEnvs[$((idx + 2))].value=argo-watcher-e2e" \
              --set-string "extraEnvs[$((idx + 3))].name=OIDC_REQUIRE_TASK_READ_AUTH" \
              --set-string "extraEnvs[$((idx + 3))].value=true"
wait_service || die "argo-watcher never came back after requiring auth on the task lookup"

req GET "${AW_API}/tasks/${UNKNOWN_TASK}"
if [[ "$CODE" == "401" ]]; then
  ok "GET /tasks/<id> -> 401 without a credential once the lookup is closed"
else
  bad "GET /tasks/<id> with the lookup closed: code=${CODE} body=${BODY} (want 401)"
fi

req GET "${AW_API}/tasks/${UNKNOWN_TASK}" -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}"
if [[ "$CODE" == "404" ]]; then
  ok "GET /tasks/<id> -> 404 with a deploy token (reached the handler)"
else
  bad "GET /tasks/<id> with a deploy token: code=${CODE} body=${BODY} (want 404 task not found)"
fi

# Both machine credentials must work here, not just the one the client runs below:
# a pipeline authenticating with BEARER_TOKEN polls the same lookup.
req GET "${AW_API}/tasks/${UNKNOWN_TASK}" -H "Authorization: ${jwt}"
if [[ "$CODE" == "404" ]]; then
  ok "GET /tasks/<id> -> 404 with a JWT (reached the handler)"
else
  bad "GET /tasks/<id> with a JWT: code=${CODE} body=${BODY} (want 404 task not found)"
fi

# /config bootstraps the login flow, so closing the lookup must not close it.
req GET "${AW_API}/config"
[[ "$CODE" == "200" ]] || bad "GET /config with the lookup closed: code=${CODE} (want 200)"
ok "GET /config stays open"

# The real client against the closed endpoint. MISSING_APP keeps this phase free of
# application state: the task reaches "app not found" without deploying anything, and
# reaching that verdict at all proves the client's poll was served. A rejected poll
# reports the 401 instead, which is exactly what the second run asserts.
MISSING_APP="no-such-app-read-auth"
build_client "$bin_dir" || die "client build failed"

out="$(run_client "$MISSING_APP" "v0.0.1" ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" || true)"
if grep -q "does not exist" <<<"$out"; then
  ok "the client polled the closed lookup with its deploy token"
else
  bad "client with a deploy token did not reach a task verdict: $(tr '\n' ',' <<<"$out")"
fi

# Both credentials are cleared explicitly: run_client does not scrub the environment,
# so a token exported by the developer or the runner would turn this into a
# credentialed run and the assertion would fail for the wrong reason.
out="$(run_client "$MISSING_APP" "v0.0.1" ARGO_WATCHER_DEPLOY_TOKEN= BEARER_TOKEN= || true)"
if grep -q "argo-watcher returned status 401" <<<"$out"; then
  ok "a client with no credential is rejected (the cost of closing the lookup)"
else
  bad "client without a credential was not rejected: $(tr '\n' ',' <<<"$out")"
fi

# --- 10. Revert and prove the toggle is the only thing gating reads -----------
revert
trap - EXIT

for path in "${PROTECTED[@]}"; do
  req GET "${AW_API}/${path}"
  [[ "$CODE" == "200" ]] || bad "after revert GET /${path}: code=${CODE}, want 200"
done
ok "every read is open again with OIDC disabled"

# Nothing above could authorize a lock, so the lab must be left unlocked.
req GET "${AW_API}/deploy-lock"
if jq -e '. == false' <<<"$BODY" >/dev/null 2>&1; then
  ok "deploy lock still unset"
else
  bad "deploy lock is set after the phase (body=${BODY})"
fi

phase_end READ-AUTH
