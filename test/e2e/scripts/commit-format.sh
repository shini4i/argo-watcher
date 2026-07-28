#!/usr/bin/env bash
# Prove COMMIT_MESSAGE_FORMAT: argo-watcher renders a user-supplied Go template
# (against the deploy task) into the git write-back commit message. argo-watcher
# runs with COMMIT_MESSAGE_FORMAT="e2e-commit-fmt app={{.App}} by={{.Author}}"; we
# drive an authenticated deploy that forces a real write-back, then read the commit
# it produced from the gitops repo and assert the rendered message.
#
# The malformed-template fallback (a bad template must not abort the deploy, it
# falls back to the default message) needs a DIFFERENT server env than the valid
# format asserted here, so it cannot be exercised in the same lab run; it stays
# covered at the unit level (internal/updater).
#
# Usage: commit-format.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

APP="app3"
AUTHOR="commitfmt-tester"
# Must match COMMIT_MESSAGE_FORMAT in values/argo-watcher.yaml, rendered for this
# deploy (App=app3, Author=commitfmt-tester).
WANT_MSG="e2e-commit-fmt app=${APP} by=${AUTHOR}"

bin_dir="$(mktemp -d)"
work="$(mktemp -d)"
trap 'rm -rf "$bin_dir" "$work"' EXIT
build_client "$bin_dir" || die "client build failed"

require_app_synced "$APP"

# Deploy a tag differing from the current one so the write-back actually commits
# (an unchanged tag is byte-compared and skipped).
TAG="$(other_tag "$APP")"

wait_service || die "argo-watcher never answered on ${AW_URL}"

echo "deploying ${APP} -> ${IMAGE}:${TAG} (author=${AUTHOR}) to force a write-back commit"
if ! run_client "$APP" "$TAG" \
     COMMIT_AUTHOR="$AUTHOR" \
     ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN"; then
  echo "COMMIT-FORMAT: FAIL (deploy did not reach 'deployed', no commit to inspect)"
  exit 1
fi

# Read the subject of the last commit touching this app's override file.
gitops_clone "${work}/r"
got=$(cd "${work}/r" && git log -1 --format=%s -- "chart/.argocd-source-${APP}.yaml")

echo "  want: ${WANT_MSG}"
echo "  got:  ${got}"
if [[ "$got" == "$WANT_MSG" ]]; then
  echo "COMMIT-FORMAT: PASS (write-back commit carries the rendered template)"
  exit 0
fi
echo "COMMIT-FORMAT: FAIL (commit message did not match the rendered template)"
exit 1
