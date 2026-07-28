#!/usr/bin/env bash
# Add or remove a deliberately-failing PreSync hook in the shared gitops chart, so a deploy
# through argo-watcher makes ArgoCD run (and fail) the hook — exercising the failed-hook branch
# of the failure-reason diagnostics. The hook is injected via the `app` chart's rawObject values
# passthrough (no chart templates needed). Shared values affect all fixture apps; callers run
# this only around the hook scenario and remove it immediately after.
#
# Usage: hook-fixture.sh <add|remove> [app]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

ACTION="${1:?usage: hook-fixture.sh <add|remove> [app]}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

gitea_ready() {
  curl -sf -m3 -u "${GITEA_ADMIN}:${GITEA_PW}" "${GITEA_API}/version" >/dev/null 2>&1
}
retry 20 1 gitea_ready || die "gitea API not reachable on ${GITEA_URL}"

gitops_clone "${work}/r"

values="$work/r/chart/values.yaml"
case "$ACTION" in
  add)
    cat > "$values" <<'YAML'
app:
  image:
    repository: traefik/whoami
    tag: v1.10.1
  rawObject:
    - apiVersion: batch/v1
      kind: Job
      metadata:
        name: presync-migration
        annotations:
          argocd.argoproj.io/hook: PreSync
          argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
      spec:
        backoffLimit: 0
        template:
          spec:
            restartPolicy: Never
            containers:
              - name: migrate
                image: busybox:1.36
                command: ["sh", "-c", "echo running db migration; sleep 2; echo 'migration failed'; exit 1"]
YAML
    msg="add failing PreSync hook fixture"
    ;;
  remove)
    cat > "$values" <<'YAML'
app:
  image:
    repository: traefik/whoami
    tag: v1.10.1
YAML
    msg="remove failing PreSync hook fixture"
    ;;
  *) echo "unknown action: $ACTION" >&2; exit 2 ;;
esac

# Idempotent: if the fixture is already in the target state there is nothing to commit, and
# `git commit` under `set -e` would abort. Only commit+push when the tree actually changed.
if git -C "$work/r" diff --quiet; then
  echo "hook-fixture: ${ACTION} already applied (no-op)"
else
  git -C "$work/r" -c user.name=e2e -c user.email=e2e@e2e commit -qam "$msg"
  git -C "$work/r" push -q origin main
  echo "hook-fixture: ${ACTION} committed"
fi
