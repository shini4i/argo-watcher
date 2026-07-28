#!/usr/bin/env bash
# Exercise client env-var knobs that no other phase covers, in a single real
# deploy through the actual cmd/client binary:
#   - TASK_REFRESH=false : per-task override of the server's refresh default
#     (issue #334). Assertion: the deploy still reaches "deployed" (client exit 0),
#     proving the server honours the override path instead of erroring on it.
#   - DEBUG=true         : the client logs an equivalent cURL command for
#     troubleshooting. Assertion: the auth header is shown as "<redacted>" and the
#     real deploy-token value never appears in the output (commit 38d86ec) — this
#     output routinely lands in CI job logs, so a leak here is a real exposure.
#
# Usage: client-knobs.sh [app] [tag]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="${1:-app1}"
# Any tag the fixture image actually has works — the assertions are the client's
# exit code and its debug output, not a specific tag transition. v1.10.2 matches
# the smoke tag so this phase is a no-op write-back if it runs after smoke.
TAG="${2:-v1.10.2}"

bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
build_client "$bin_dir" || die "client build failed"

require_app_synced "$APP"
wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> ${IMAGE}:${TAG} with TASK_REFRESH=false DEBUG=true"

# Capture combined output AND exit code: the exit code is the TASK_REFRESH
# assertion, the output is the DEBUG-redaction assertion.
set +e
out=$(run_client "$APP" "$TAG" \
  ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" \
  TASK_REFRESH="false" \
  DEBUG="true")
rc=$?
set -e
echo "$out"

if [[ "$rc" -eq 0 ]]; then
  ok "TASK_REFRESH=false deploy reached 'deployed' (exit 0)"
else
  bad "client exited ${rc} with TASK_REFRESH=false (did not reach 'deployed')"
fi

# Go canonicalises the header key on Header.Set, so it appears as
# "Argo_watcher_deploy_token" in the log — match case-insensitively.
if grep -qiE "argo_watcher_deploy_token: <redacted>" <<<"$out"; then
  ok "DEBUG cURL log redacts the deploy-token header"
else
  bad "DEBUG cURL log did not show the redacted deploy-token header"
fi
if grep -qF "$DEPLOY_TOKEN" <<<"$out"; then
  bad "deploy token leaked verbatim into client output"
else
  ok "deploy token value never appears in client output"
fi

phase_end CLIENT-KNOBS
