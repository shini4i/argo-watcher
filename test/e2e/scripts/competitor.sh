#!/usr/bin/env bash
# Competitor writer: pushes noise commits to the shared gitops repo throughout
# the soak, so argo-watcher's (per-repo-serialized) write-back keeps hitting
# non-fast-forward rejections and exercises its retry loop. This is what
# reproduces the real-world conflict — argo-watcher does not race itself.
#
# The noise lands at repo root (competitor.log), outside the chart/ path Argo
# renders, so it advances the branch HEAD without disturbing the deployed apps.
set -uo pipefail

SECONDS_TOTAL="${SECONDS_TOTAL:-300}"
INTERVAL="${INTERVAL:-1}"
ORG="${ORG:-e2e}"
REPO="${REPO:-gitops}"
HTTP_PORT="${HTTP_PORT:-13000}"
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"

kubectl -n gitea port-forward svc/gitea-http "${HTTP_PORT}:3000" >/dev/null 2>&1 &
trap 'kill $(jobs -p) 2>/dev/null || true' EXIT
for _ in $(seq 1 20); do
  curl -sf -m 2 "http://localhost:${HTTP_PORT}/api/healthz" >/dev/null 2>&1 && break; sleep 1
done

work="$(mktemp -d)"
url="http://${GITEA_ADMIN}:${GITEA_PW}@localhost:${HTTP_PORT}/${ORG}/${REPO}.git"
git clone -q "$url" "${work}/r" || { echo "competitor: clone failed" >&2; exit 1; }
cd "${work}/r"
git config user.name competitor
git config user.email competitor@e2e

n=0 pushes=0 conflicts=0
end=$(( $(date +%s) + SECONDS_TOTAL ))
while [ "$(date +%s)" -lt "$end" ]; do
  git fetch -q origin main && git reset -q --hard origin/main
  n=$((n + 1))
  echo "$n $(date +%s)" >> competitor.log
  git add competitor.log && git commit -q -m "competitor ${n}"
  if git push -q "$url" HEAD:main 2>/dev/null; then
    pushes=$((pushes + 1))
  else
    conflicts=$((conflicts + 1))   # argo-watcher pushed first; re-sync next loop
  fi
  sleep "$INTERVAL"
done
echo "competitor: commits=${n} pushes=${pushes} lost-races=${conflicts}"
