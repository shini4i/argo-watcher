#!/usr/bin/env bash
# Inject (or remove) a deliberately-broken resource in the shared gitops chart, so a deploy
# through argo-watcher makes real ArgoCD produce a specific failure — exercising one branch of
# the failure-reason diagnostics. Resources are injected via the `app` chart's rawObject values
# passthrough (no chart templates needed).
#
# Actions:
#   hook      a failing PreSync hook Job. The hook aborts the sync before the main wave, so the
#             app keeps its old image and the failure surfaces under "Failed resources:".
#   degraded  a failing PLAIN Job (no hook annotations). It applies in the same wave as the
#             Deployment, so the new image DOES roll out while the Job exhausts its backoff
#             limit: the app settles Synced + Degraded, the terminal-degraded rollout.
#   pending   an extra Deployment whose readiness probe never passes. Its container stays up, so
#             nothing is ever Degraded — the app sits Synced + Progressing until the task times
#             out, the rollout whose diagnostics have no failing resource to name.
#   remove    restore the baseline chart values (no injected resource).
#
# This script is the single owner of the shared chart/values.yaml — every action rewrites the
# whole file, so a second script writing it would clobber whichever ran first. The values are
# shared by ALL fixture apps, so callers add a fixture only around their own scenario and remove
# it immediately after.
#
# Usage: failure-fixture.sh <hook|degraded|pending|remove>
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

ACTION="${1:?usage: failure-fixture.sh <hook|degraded|pending|remove>}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

gitea_ready() {
  curl -sf -m3 -u "${GITEA_ADMIN}:${GITEA_PW}" "${GITEA_API}/version" >/dev/null 2>&1
}
retry 20 1 gitea_ready || die "gitea API not reachable on ${GITEA_URL}"

gitops_clone "${work}/r"

values="$work/r/chart/values.yaml"
case "$ACTION" in
  hook)
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
  degraded)
    # No hook annotations on purpose: a plain Job applies alongside the Deployment instead of
    # gating the sync, so the deploy's image lands and the app settles Synced + Degraded. Its
    # spec never changes between syncs (the image is pinned here, not driven by write-back), so
    # the Job is created once, fails once, and holds the app Degraded until it is removed — no
    # immutable-field sync error, and the same state on every re-run.
    cat > "$values" <<'YAML'
app:
  image:
    repository: traefik/whoami
    tag: v1.10.1
  rawObject:
    - apiVersion: batch/v1
      kind: Job
      metadata:
        name: failing-migration
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
    msg="add failing migration Job fixture"
    ;;
  pending)
    # The container sleeps rather than exiting, so it never crashes and ArgoCD never calls
    # anything Degraded; only the always-failing readiness probe keeps the pod out of Ready,
    # which ArgoCD reports as Progressing. progressDeadlineSeconds is deliberately far longer
    # than any task timeout in this phase: once that deadline passes Kubernetes sets
    # ProgressDeadlineExceeded and ArgoCD flips the Deployment to Degraded, which would
    # exercise the degraded branch instead of the still-progressing one under test.
    cat > "$values" <<'YAML'
app:
  image:
    repository: traefik/whoami
    tag: v1.10.1
  rawObject:
    - apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: never-ready
      spec:
        replicas: 1
        progressDeadlineSeconds: 1800
        selector:
          matchLabels:
            app: never-ready
        template:
          metadata:
            labels:
              app: never-ready
          spec:
            containers:
              - name: pause
                image: busybox:1.36
                command: ["sh", "-c", "sleep 3600"]
                readinessProbe:
                  exec:
                    command: ["false"]
                  periodSeconds: 5
YAML
    msg="add never-ready Deployment fixture"
    ;;
  remove)
    cat > "$values" <<'YAML'
app:
  image:
    repository: traefik/whoami
    tag: v1.10.1
YAML
    msg="remove failure fixture"
    ;;
  *) echo "unknown action: $ACTION" >&2; exit 2 ;;
esac

# Idempotent: if the fixture is already in the target state there is nothing to commit, and
# `git commit` under `set -e` would abort. Only commit+push when the tree actually changed.
if git -C "$work/r" diff --quiet; then
  echo "failure-fixture: ${ACTION} already applied (no-op)"
else
  git -C "$work/r" -c user.name=e2e -c user.email=e2e@e2e commit -qam "$msg"
  git -C "$work/r" push -q origin main
  echo "failure-fixture: ${ACTION} committed"
fi
