#!/usr/bin/env bash
# Same-app supersession race, driven by the REAL argo-watcher client (cmd/client).
#
# Fires an OLDER then a NEWER deploy for the same app while a competitor keeps the
# write-back retrying (scripts/competitor.sh), and asserts:
#   * the NEWER deploy's client exits 0 (it deployed),
#   * the OLDER deploy's client exits non-zero AND reports it was superseded
#     (server marks the superseded task "cancelled" -> client's cancelled branch),
#   * the tag committed to the gitops repo is the NEWER one — i.e. the older task's
#     retry never clobbered the winner.
# Without the supersession guard in the write-back loop, the older task could commit
# its stale tag last. Exits non-zero on any violation.
#
# The script is self-contained and order-independent: it first resets the app to a
# BASE_TAG distinct from OLD and NEW, so the OLD deploy ALWAYS triggers a real,
# supersede-able write-back regardless of what tag earlier phases (the soak) left
# the app on. Without this, if the app already sits on OLD_TAG, the OLD deploy is a
# no-op fast-path (unchanged tags skip write-back since #472), completes instantly,
# and is never superseded. It then starts the competitor itself so contention is
# active before the race fires.
#
# Required env: GITEA_REPO_URL (gitops repo clone URL, creds inline).
# Optional env: BASE_URL, DEPLOY_TOKEN, APP, BASE_TAG, OLD_TAG, NEW_TAG, IMAGE,
#               COMPETITOR_INTERVAL, COMPETITOR_SECONDS, COMPETITOR_HTTP_PORT.
set -uo pipefail

APP="${APP:-app1}"
BASE_TAG="${BASE_TAG:-v1.10.2}"
OLD_TAG="${OLD_TAG:-v1.10.1}"
NEW_TAG="${NEW_TAG:-v1.10.3}"
IMAGE="${IMAGE:-traefik/whoami}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
BASE_URL="${BASE_URL:-http://localhost:8080}"
GITEA_REPO_URL="${GITEA_REPO_URL:?GITEA_REPO_URL required (gitops repo clone URL)}"
COMPETITOR_INTERVAL="${COMPETITOR_INTERVAL:-1}"
COMPETITOR_SECONDS="${COMPETITOR_SECONDS:-90}"
COMPETITOR_HTTP_PORT="${COMPETITOR_HTTP_PORT:-13010}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

# BASE_TAG, OLD_TAG, NEW_TAG must all differ: BASE ≠ OLD/NEW so both race deploys
# force a real write-back; OLD ≠ NEW so NEW genuinely supersedes OLD.
if [[ "$BASE_TAG" == "$OLD_TAG" || "$BASE_TAG" == "$NEW_TAG" || "$OLD_TAG" == "$NEW_TAG" ]]; then
  echo "race: BASE_TAG/OLD_TAG/NEW_TAG must all differ (base=${BASE_TAG} old=${OLD_TAG} new=${NEW_TAG})" >&2
  exit 1
fi

# Build the client once so both deploys launch a prebuilt binary — a per-invocation
# `go run` compile would blow the sub-second submission ordering the race needs.
bin_dir="$(mktemp -d)"
base_out="$(mktemp)"; old_out="$(mktemp)"; new_out="$(mktemp)"; clone_dir="$(mktemp -d)"
CLIENT_BIN="${bin_dir}/aw-client"
comp_pid=""
trap '[ -n "$comp_pid" ] && kill "$comp_pid" 2>/dev/null; rm -rf "$bin_dir" "$clone_dir" "$base_out" "$old_out" "$new_out"' EXIT
( cd "$root" && go build -o "$CLIENT_BIN" ./cmd/client ) || { echo "race: FAIL — client build failed" >&2; exit 1; }

# deploy <tag> <outfile>: run the client to deploy APP:tag, blocking to a terminal
# status. Combined stdout+stderr goes to outfile; the client's exit code propagates.
deploy() {
  local tag="$1" out="$2"
  env ARGO_WATCHER_URL="$BASE_URL" \
      IMAGES="$IMAGE" IMAGE_TAG="$tag" ARGO_APP="$APP" \
      COMMIT_AUTHOR="e2e" PROJECT_NAME="lab" \
      ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" \
      RETRY_INTERVAL="5s" TASK_TIMEOUT="180" \
      "$CLIENT_BIN" >"$out" 2>&1
  return
}

# 1) Baseline: pin the app to BASE_TAG (no competitor yet, so this is fast) so the
#    OLD deploy below is guaranteed to be a real, supersede-able write-back.
echo "race: resetting ${APP} to baseline ${BASE_TAG} before the race"
if ! deploy "$BASE_TAG" "$base_out"; then
  echo "race: FAIL — baseline deploy of ${BASE_TAG} did not reach 'deployed'" >&2
  sed 's/^/  | /' "$base_out"
  exit 1
fi

# 2) Start the competitor to force write-back contention, and give it a moment to
#    clone and begin advancing origin/main before the race deploys run.
HTTP_PORT="$COMPETITOR_HTTP_PORT" SECONDS_TOTAL="$COMPETITOR_SECONDS" INTERVAL="$COMPETITOR_INTERVAL" \
  "${here}/competitor.sh" & comp_pid=$!
sleep 5

# 3) Race: fire OLD then NEW; NEW must supersede the still-retrying OLD.
echo "race: ${APP} <- OLD ${OLD_TAG} then NEW ${NEW_TAG} (competitor forces write-back retries)"
deploy "$OLD_TAG" "$old_out" & old_pid=$!
sleep 0.3   # ensure NEW is submitted after OLD so it supersedes it
deploy "$NEW_TAG" "$new_out" & new_pid=$!
wait "$old_pid"; old_rc=$?
wait "$new_pid"; new_rc=$?

echo "OLD ${OLD_TAG}: exit=${old_rc}"; sed 's/^/  | /' "$old_out"
echo "NEW ${NEW_TAG}: exit=${new_rc}"; sed 's/^/  | /' "$new_out"

# Read the tag currently committed in the app's override file.
git clone -q "$GITEA_REPO_URL" "$clone_dir" || { echo "race: FAIL — gitops clone failed" >&2; exit 1; }
override="${clone_dir}/chart/.argocd-source-${APP}.yaml"
# The file is small controlled YAML (helm.parameters: [{name, value, forceString}]).
# awk keeps this yq-free (yq is not on GitHub runners): after the app.image.tag
# name line, take the value on the next `value:` line, stripping any quotes.
committed=$(awk '/name:[[:space:]]*app\.image\.tag/{f=1} f&&/value:/{v=$NF; gsub(/"/,"",v); print v; exit}' "$override")
# Distinguish a parse failure (key renamed / file missing / non-consecutive lines)
# from a genuine supersede violation — an empty result would otherwise masquerade
# as "committed tag <none> is not the newer tag" below.
if [[ -z "$committed" ]]; then
  echo "race: FAIL — could not read app.image.tag from ${override} (parse failure, not a supersede result)" >&2
  exit 1
fi
echo "race: committed git tag=${committed}"

rc=0
[[ "$new_rc" -eq 0 ]] || { echo "race: FAIL — NEW ${NEW_TAG} client exited ${new_rc}, expected 0 (deployed)"; rc=1; }
[[ "$old_rc" -ne 0 ]] || { echo "race: FAIL — OLD ${OLD_TAG} client exited 0, expected non-zero (superseded)"; rc=1; }
if ! grep -qiE "supersed|cancel" "$old_out"; then
  echo "race: FAIL — OLD client output did not report the deploy was superseded/cancelled"; rc=1
fi
[[ "$committed" == "$NEW_TAG" ]] || { echo "race: FAIL — committed tag ${committed:-<none>} is not the newer ${NEW_TAG} (superseded task may have clobbered the winner)"; rc=1; }

if [[ "$rc" -eq 0 ]]; then
  echo "race OK: newer tag ${NEW_TAG} won; older ${OLD_TAG} was superseded and did not clobber it"
fi
exit "$rc"
