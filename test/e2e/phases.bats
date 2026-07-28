#!/usr/bin/env bats
# The per-release phase suite. Each phase is one test, run in the order below
# against a lab that is already up (`task up`); `task e2e` wraps up → this → down.
#
# Once a phase fails, the rest are SKIPPED rather than run: a phase driven against an
# already-broken lab only produces cascading noise, and `task e2e` then stops before
# `down` so the cluster is left up for debugging. bats has no fail-fast flag (there is
# no --abort in 1.12), so setup/teardown below carry the state in a marker file.
#
# The test IS the phase, not each assertion inside it: bats runs every test in its
# own subshell, and a phase like lockdown is one sequential scenario (enable a
# schedule, observe the transition, revert) whose steps cannot be split without
# either breaking it or re-running minutes of setup per assertion. Per-phase tests
# still give CI a JUnit case per phase and make `--filter` / `--filter-status failed`
# work for reruns.
#
# ORDER IS LOAD-BEARING. Each comment below states what constrains a phase's
# position; do not reorder without re-reading them.

bats_require_minimum_version 1.5.0

setup_file() {
  export AW_CHART_REPO="${AW_CHART_REPO:?AW_CHART_REPO is required (set by the Taskfile)}"
  export AW_CHART_VERSION="${AW_CHART_VERSION:?AW_CHART_VERSION is required (set by the Taskfile)}"
  export DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
  export JWT_SECRET="${JWT_SECRET:-e2e-jwt-secret}"
  export WEBHOOK_UUID="${WEBHOOK_UUID:-11111111-1111-1111-1111-111111111111}"
}

# soak_env: the tuning knobs for the two contention soaks, named SOAK_*/BATCH_* by the
# Taskfile so they cannot leak into phases that read the same variable under its plain
# name. race-supersede.sh's COMPETITOR_INTERVAL and shutdown-drain.sh's WS_CLIENTS are
# deliberately tuned per phase; a suite-wide export would silently override them.
soak_env() {
  echo "APPS=${SOAK_APPS:-5}"
  echo "WORKERS=${SOAK_WORKERS:-10}"
  echo "WS_CLIENTS=${SOAK_WS_CLIENTS:-10}"
  echo "COMPETITOR_INTERVAL=${SOAK_COMPETITOR_INTERVAL:-2}"
}

setup() {
  # BATS_FILE_TMPDIR is shared by every test in this file, so the marker survives
  # between them (each test body runs in its own subshell).
  [[ -f "${BATS_FILE_TMPDIR}/aborted" ]] && skip "an earlier phase failed"
  cd "${BATS_TEST_DIRNAME}" || return 1
}

teardown() {
  # BATS_TEST_COMPLETED is 1 only when the test body ran to the end.
  [[ "${BATS_TEST_COMPLETED:-}" == 1 ]] || touch "${BATS_FILE_TMPDIR}/aborted"
}

# phase <script> [args...]: run a phase script; its exit status is the test result.
#
# Output goes to FD 3, which bats passes straight through, because a phase can run
# for minutes (the soak is 5) and bats otherwise buffers a test's output until it
# finishes — the run would look hung.
phase() {
  ./scripts/"$@" >&3 2>&3
}

@test "api-surface: read-only HTTP surface to contract" {
  # Touches no state, so it runs first as a fast contract check.
  phase api-surface.sh
}

@test "smoke: one authenticated deploy through the write-back loop" {
  phase smoke-deploy.sh
}

@test "client-knobs: TASK_REFRESH override deploys, DEBUG log redacts the token" {
  phase client-knobs.sh
}

@test "jwt-auth: the BEARER_TOKEN path drives an authenticated write-back" {
  phase jwt-auth.sh
}

@test "fire-and-forget: a managed CronJob reports deployed without rolling out" {
  phase fire-and-forget.sh
}

@test "commit-format: COMMIT_MESSAGE_FORMAT renders into the write-back commit" {
  phase commit-format.sh
}

@test "multi-image: both images deploy and both tags are written back" {
  phase multi-image.sh
}

@test "accept-suspended: a paused Rollout is accepted as deployed" {
  phase accept-suspended.sh
}

@test "docker-proxy: a bare image name matches the proxy-prefixed running image" {
  phase docker-proxy.sh
}

@test "lockdown: LOCKDOWN_SCHEDULE freezes deploys and broadcasts 'locked'" {
  # Toggles a server-global freeze on the release and reverts before returning
  # (ending unlocked), so it must precede the soak. Otherwise self-contained.
  phase lockdown.sh
}

@test "notifications: the generic webhook fires start + result with the right payload" {
  # Needs a clean state and asserts on its own uniquely-tagged deploy, so it runs
  # before the soak dirties the shared receiver.
  phase notifications.sh
}

@test "load: git-conflict soak with zero failed tasks and zero lost updates" {
  env $(soak_env) SOAK="${SOAK_DURATION:-5m}" SOAK_SECONDS="${SOAK_SECONDS:-300}" \
    ./scripts/soak.sh >&3 2>&3
}

@test "batch-writeback: GIT_BATCH_WRITEBACK coalesces under contention" {
  # Re-runs the contention soak with batching on and reverts before returning, so it
  # must follow the default-path soak above. Its soak is deliberately shorter: 10
  # workers on one repo coalesce quickly, so a 2m window shows batching without
  # doubling the run.
  env $(soak_env) SOAK="${BATCH_SOAK:-2m}" SOAK_SECONDS="${BATCH_SOAK_SECONDS:-120}" \
    ./scripts/batch-writeback.sh >&3 2>&3
}

@test "race: a newer deploy supersedes an older retrying one" {
  phase race-supersede.sh
}

@test "supersede-authority: an uncredentialed task cannot cancel a credentialed one" {
  # Pairs with the race phase above: that one proves supersession works, this one
  # proves it cannot be driven by an anonymous caller. Self-contained on app2.
  phase supersede-authority.sh
}

@test "state-postgres: migration, deploy loop, task survives restart, shared lock" {
  # Flips the release to Postgres and asserts the Postgres-only properties. Placed
  # after the in-memory phases (which validate that backend) and BEFORE
  # failure-diagnostics so it deploys against pristine apps. Everything after runs on
  # Postgres — both remaining phases are backend-agnostic, so that is a free bonus.
  phase state-postgres.sh
}

@test "failure-diagnostics: failure reasons carry the real cause" {
  # Deliberately breaks apps (bad images, a failing PreSync hook in the SHARED chart
  # values) and pushes straight to the gitops repo, so running it before the soak
  # would perturb those pristine-state gates.
  phase failure-diagnostics.sh
}

@test "argocd-unreachable: /reachability flips, WS broadcasts, POST fast-fails 503" {
  # Severs ArgoCD (scales argocd-server to 0) then restores it, so it must follow
  # every phase that needs a healthy ArgoCD. Its cleanup trap always scales ArgoCD
  # back and its tokenless deploys leave nothing behind.
  phase argocd-unreachable.sh
}

@test "shutdown-drain: graceful shutdown drains in-flight WebSockets" {
  # LAST: it deletes the argo-watcher pod, so nothing but `down` may depend on the
  # server afterwards.
  phase shutdown-drain.sh
}
