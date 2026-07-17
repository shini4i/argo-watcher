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

APP="${1:-app1}"
# Any tag the fixture image actually has works — the assertions are the client's
# exit code and its debug output, not a specific tag transition. v1.10.2 matches
# the smoke tag so this phase is a no-op write-back if it runs after smoke.
TAG="${2:-v1.10.2}"
IMAGE="${IMAGE:-traefik/whoami}"
NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18093}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

bin_dir="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
( cd "$root" && go build -o "${bin_dir}/aw-client" ./cmd/client )

for _ in $(seq 1 40); do
  s=$(kubectl -n argocd get application "$APP" -o jsonpath='{.status.sync.status}/{.status.health.status}' 2>/dev/null || true)
  [[ "$s" == "Synced/Healthy" ]] && break
  sleep 5
done
[[ "$s" == "Synced/Healthy" ]] || { echo "CLIENT-KNOBS: FAIL — ${APP} never reached Synced/Healthy (last: ${s:-unknown})"; exit 1; }

kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m 3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

echo "deploying ${APP} -> ${IMAGE}:${TAG} with TASK_REFRESH=false DEBUG=true"

# Capture combined output AND exit code: the exit code is the TASK_REFRESH
# assertion, the output is the DEBUG-redaction assertion.
set +e
out=$(ARGO_WATCHER_URL="http://localhost:${PORT}" \
   IMAGES="${IMAGE}" \
   IMAGE_TAG="${TAG}" \
   ARGO_APP="${APP}" \
   COMMIT_AUTHOR="e2e" \
   PROJECT_NAME="lab" \
   ARGO_WATCHER_DEPLOY_TOKEN="${DEPLOY_TOKEN}" \
   TASK_REFRESH="false" \
   DEBUG="true" \
   RETRY_INTERVAL="5s" \
   TASK_TIMEOUT="180" \
   "${bin_dir}/aw-client" 2>&1)
rc=$?
set -e
echo "$out"

fail=0
if [[ "$rc" -eq 0 ]]; then
  echo "  OK   TASK_REFRESH=false deploy reached 'deployed' (exit 0)"
else
  echo "  FAIL client exited ${rc} with TASK_REFRESH=false (did not reach 'deployed')"; fail=1
fi

# Go canonicalises the header key on Header.Set, so it appears as
# "Argo_watcher_deploy_token" in the log — match case-insensitively.
if grep -qiE "argo_watcher_deploy_token: <redacted>" <<<"$out"; then
  echo "  OK   DEBUG cURL log redacts the deploy-token header"
else
  echo "  FAIL DEBUG cURL log did not show the redacted deploy-token header"; fail=1
fi
if grep -qF "$DEPLOY_TOKEN" <<<"$out"; then
  echo "  FAIL deploy token leaked verbatim into client output"; fail=1
else
  echo "  OK   deploy token value never appears in client output"
fi

if [[ "$fail" -eq 0 ]]; then echo "CLIENT-KNOBS: PASS"; else echo "CLIENT-KNOBS: FAIL"; exit 1; fi
