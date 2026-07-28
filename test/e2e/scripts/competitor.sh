#!/usr/bin/env bash
# Competitor writer: pushes noise commits to the shared gitops repo throughout
# the soak, so argo-watcher's (per-repo-serialized) write-back keeps hitting
# non-fast-forward rejections and exercises its retry loop. This is what
# reproduces the real-world conflict — argo-watcher does not race itself.
#
# The noise lands at repo root (competitor.log), outside the chart/ path Argo
# renders, so it advances the branch HEAD without disturbing the deployed apps.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

SECONDS_TOTAL="${SECONDS_TOTAL:-300}"
INTERVAL="${INTERVAL:-1}"

# Explicit -f predicate rather than wait_url: Gitea's healthz has no 503-by-design
# case, so "answers at all" would let a 5xx during startup through.
gitea_up() { curl -sf -m 3 -o /dev/null "${GITEA_URL}/api/healthz"; }
retry 20 1 gitea_up || die "gitea not healthy on ${GITEA_URL}"

work="$(mktemp -d)"
gitops_clone "${work}/r"
cd "${work}/r" || die "could not enter the clone"
git config user.name competitor
git config user.email competitor@e2e

n=0 pushes=0 conflicts=0
end=$(( $(date +%s) + SECONDS_TOTAL ))
while [[ "$(date +%s)" -lt "$end" ]]; do
  git fetch -q origin main && git reset -q --hard origin/main
  n=$((n + 1))
  echo "$n $(date +%s)" >> competitor.log
  git add competitor.log && git commit -q -m "competitor ${n}"
  if git push -q "$GITOPS_REPO_URL" HEAD:main 2>/dev/null; then
    pushes=$((pushes + 1))
  else
    conflicts=$((conflicts + 1))   # argo-watcher pushed first; re-sync next loop
  fi
  sleep "$INTERVAL"
done
echo "competitor: commits=${n} pushes=${pushes} lost-races=${conflicts}"
