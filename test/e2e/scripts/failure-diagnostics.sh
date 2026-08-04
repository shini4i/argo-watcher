#!/usr/bin/env bash
# Assert the REAL argo-watcher client (cmd/client) surfaces an ACTIONABLE failure
# reason from real ArgoCD — not just that the server stored one. Each scenario
# drives a deployment meant to fail a specific way, runs the client binary against
# it, and checks the client (a) exits non-zero and (b) prints the substrings that
# pin the diagnosis. This exercises the client's failure path end-to-end (the
# waitForDeployment "failed" branch + handleDeploymentError), which raw curl polls
# never did.
#
# Table-driven on purpose: adding coverage is one entry in SCENARIOS plus a
# scenario_<name> function. Each scenario echoes, on stdout, "key=value" lines the
# runner reads:
#   task=<json>            deploy payload (app/author/project/timeout/images)  (required)
#   token=<0|1>            send the deploy token (enables write-back); default 1
#   expect=<substring>     a substring that MUST appear in the client output (repeatable)
#   max_seconds=<n>        optional: the client must return within n seconds
#   setup / teardown       optional: names of functions run before/after the scenario
# The runner runs the client, captures its combined output + exit code, and greps.
#
# Usage: DEPLOY_TOKEN=... failure-diagnostics.sh
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
. "${here}/lib.sh"

GOOD_TAG="${GOOD_TAG:-v1.10.1}"

# Build the client once; every scenario runs the same binary (deterministic, no
# per-invocation `go run` compile).
bin_dir="$(mktemp -d)"
# Revert the shared chart values on ANY exit, not just the happy path: a scenario setup that dies
# (its app never reaching the expected state) would otherwise leave a repo-wide fixture committed,
# and a failing phase deliberately leaves the cluster up — so the next run would start from a broken
# baseline. The diagnosis lives in the captured client output, not in the live cluster state.
#
# Gated on a fixture actually having been applied, so a phase that fails before that (an unreachable
# service, a failed build) does not push a pointless commit to the lab's gitops repo.
fixture_applied=0
cleanup() {
  if [[ "$fixture_applied" -eq 1 ]]; then
    "${here}/failure-fixture.sh" remove >/dev/null 2>&1 || true
  fi
  rm -rf "$bin_dir"
  return
}
trap cleanup EXIT
build_client "$bin_dir" || die "client build failed"

wait_service || die "argo-watcher not reachable on ${AW_URL}"

# --- helpers ----------------------------------------------------------------
# scenario_client <task-json> <use_token>: runs the client binary for the deploy
# described by the JSON. Prints the client's combined stdout+stderr; returns the
# client's exit code (0 = deployed, non-zero = failed/cancelled/etc.).
scenario_client() {
  local payload="$1" use_token="$2" token_env=()
  local app author project image tag timeout
  app=$(jq -r '.app' <<<"$payload")
  author=$(jq -r '.author' <<<"$payload")
  project=$(jq -r '.project' <<<"$payload")
  image=$(jq -r '.images[0].image' <<<"$payload")
  tag=$(jq -r '.images[0].tag' <<<"$payload")
  timeout=$(jq -r '.timeout // 180' <<<"$payload")
  if [[ "$use_token" == "1" ]]; then
    token_env=(ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN")
  else
    # Explicitly neutralize any ambient auth so a token=0 scenario stays
    # unvalidated even when the developer has these exported in their shell
    # (empty is treated as no token by the client). `env` does not clear
    # inherited vars on its own.
    token_env=(ARGO_WATCHER_DEPLOY_TOKEN= BEARER_TOKEN=)
  fi
  # "${token_env[@]+...}" guards the empty-array expansion under `set -u` on bash < 4.4.
  run_client "$app" "$tag" \
    IMAGES="$image" COMMIT_AUTHOR="$author" PROJECT_NAME="$project" \
    TASK_TIMEOUT="$timeout" \
    "${token_env[@]+"${token_env[@]}"}"
  return
}

# restore_good_tag <app>: bump the app back to a pullable tag so the lab stays
# reusable. Best-effort — a restore hiccup must not fail the suite.
restore_good_tag() {
  local app="$1"
  scenario_client "{\"app\":\"${app}\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":120,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${GOOD_TAG}\"}]}" 1 >/dev/null 2>&1 || true
  return
}

# --- scenarios --------------------------------------------------------------
# Committed image tag that cannot be pulled -> pods ImagePullBackOff. The cause lives ONLY on
# the Pod (resource tree), never in the app's top-level resources; the fix must surface it.
scenario_bad_image() {
  echo "task={\"app\":\"app1\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":90,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-nopull\"}]}"
  echo "expect=Unhealthy resources:"
  echo "expect=Pod("
  echo "expect=ErrImagePull"
  # On failure the client prints the ArgoCD UI link built from the server's
  # ARGO_URL_ALIAS (set in values/argo-watcher.yaml); asserting the aliased URL
  # here covers that config toggle without a dedicated phase.
  echo "expect=https://argocd.e2e.lab/applications/app1"
  echo "teardown=teardown_bad_image"
  return
}
teardown_bad_image() { restore_good_tag app1; return; }

# A deploy request whose image is never applied (unvalidated -> write-back skipped) stays
# "not available" with the app green. There is nothing for ArgoCD to diagnose, so the reason
# must be the baseline image lists WITHOUT inventing diagnostics.
scenario_unvalidated_not_available() {
  echo "task={\"app\":\"app2\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":45,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-never\"}]}"
  echo "token=0"
  echo "expect=Rollout status is not available"
  echo "expect=List of expected images:"
  return
}

# A failing PreSync hook (Helm Job) must surface as a failed hook, not just image lists.
#
# The tag MUST differ from app3's current live tag. argo-watcher only drives a sync when the
# write-back actually changes the override file (unchanged tags are a no-op since #472); a sync
# is what makes ArgoCD run the PreSync hook. The SOAK phase deterministically (fixed RNG seed)
# leaves app3 at one of ${TAGS} (v1.10.1/2/3), so we deploy a tag OUTSIDE that set to guarantee
# a real write-back regardless of which SOAK tag app3 ended on. The tag is never pulled: a failing
# PreSync hook aborts the sync before the main wave applies the Deployment — so the image stays at
# the old tag, the expected image is "not available", and the failure diagnostics carry the hook.
scenario_failed_presync_hook() {
  echo "task={\"app\":\"app3\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":90,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-hookfail\"}]}"
  echo "expect=Failed hooks:"
  echo "expect=PreSync Failed"
  echo "setup=setup_failed_presync_hook"
  echo "teardown=teardown_failed_presync_hook"
  return
}
setup_failed_presync_hook()    { fixture_applied=1; "${here}/failure-fixture.sh" hook; return; }
teardown_failed_presync_hook() { "${here}/failure-fixture.sh" remove; restore_good_tag app3; return; }

# A failing plain (non-hook) migration Job holds the app Degraded while the deploy's image
# rolls out normally. That combination — Synced, the expected image live, health Degraded — is
# the TERMINAL degraded rollout, which must be reported as a degraded rollout carrying the
# resource-level cause. Regression guard for the routing bug where this arm was reported as
# "ArgoCD API Error: application has degraded" with no diagnostics at all.
#
# app4 is used by no other scenario in this phase, so the repo-wide fixture is added and
# removed around this deploy alone. The tag must differ from the app's current one or the
# write-back is a byte-identical no-op (#472) and no sync — hence no rollout — happens.
scenario_degraded_migration_job() {
  local tag
  tag=$(other_tag app4)
  echo "task={\"app\":\"app4\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":180,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${tag}\"}]}"
  echo "expect=Rollout status is degraded"
  echo "expect=App health status \"Degraded\""
  echo "expect=Job(failing-migration) Degraded with message Job has reached the specified backoff limit"
  echo "expect=Unhealthy resources:"
  # The pod-level cause comes only from the resource tree; the pod name carries a random
  # suffix, so assert the message. Without the tree enrichment this line is absent entirely.
  echo "expect=failed with exit code 1"
  # A terminal degradation must be reported the moment it is observed, not waited out: the
  # deploy above allows 180s, so anything near it means the rollout kept polling instead.
  echo "max_seconds=120"
  echo "setup=setup_degraded_migration_job"
  echo "teardown=teardown_degraded_migration_job"
  return
}
setup_degraded_migration_job() {
  fixture_applied=1
  "${here}/failure-fixture.sh" degraded
  # Nothing is polling app4 yet, so without a nudge this would wait out ArgoCD's default 180s
  # reconciliation. The refresh annotation makes the auto-sync (and the Job's failure) prompt
  # and deterministic instead.
  refresh_all_apps
  # The Job must already be failing when the deploy starts, so the rollout observes Degraded
  # with the new image live rather than a mid-sync state that is merely not-yet-healthy.
  wait_app app4 "Degraded" 40 || die "app4 never became Degraded with the failing-migration fixture"
  return
}
teardown_degraded_migration_job() {
  "${here}/failure-fixture.sh" remove
  refresh_all_apps
  # Wait for the prune to clear the Job before restoring the tag: a deploy issued while the
  # app is still Degraded would fail and burn its whole timeout, and later phases open on a
  # Synced/Healthy baseline.
  wait_app app4 "Synced/Healthy" 60 || note "app4 did not return to Synced/Healthy (last: ${APP_STATE:-unknown})"
  restore_good_tag app4
  return
}

# A rollout that merely runs out its timeout while pods are still coming up has NOTHING
# Degraded — every node is Progressing, which the problem-health filter excludes. The reason must
# still name what never became ready instead of ending at a bare health line with no resources.
scenario_progressing_timeout() {
  local tag
  tag=$(other_tag app5)
  # A short timeout is the point: the app never becomes healthy, so the task must expire. Kept
  # well under the fixture's progressDeadlineSeconds so nothing turns Degraded first.
  echo "task={\"app\":\"app5\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":75,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${tag}\"}]}"
  echo "expect=Rollout status is not healthy"
  echo "expect=App health status \"Progressing\""
  echo "expect=Resources still progressing:"
  echo "expect=never-ready"
  echo "max_seconds=150"
  echo "setup=setup_progressing_timeout"
  echo "teardown=teardown_progressing_timeout"
  return
}
setup_progressing_timeout() {
  fixture_applied=1
  "${here}/failure-fixture.sh" pending
  refresh_all_apps
  wait_app app5 "Progressing" 40 || die "app5 never became Progressing with the never-ready fixture"
  return
}
teardown_progressing_timeout() {
  "${here}/failure-fixture.sh" remove
  refresh_all_apps
  wait_app app5 "Synced/Healthy" 60 || note "app5 did not return to Synced/Healthy (last: ${APP_STATE:-unknown})"
  restore_good_tag app5
  return
}

# refresh_all_apps: ask ArgoCD to re-compare every fixture app against git now.
#
# Scoped to ALL apps, not just the one under test: chart/values.yaml is shared, so an injected
# fixture breaks every fixture app, and removing it only clears them once each one reconciles.
# Refreshing just the app under test would leave the rest unhealthy for up to ArgoCD's 180s
# reconciliation and stall whichever phase runs next on its Synced/Healthy precondition.
refresh_all_apps() {
  kubectl -n "$NS_ARGOCD" annotate applications --all \
    argocd.argoproj.io/refresh=normal --overwrite >/dev/null 2>&1 || true
  return
}

# An image name the app never declares must fail RIGHT AWAY (issue #519) instead of burning
# the task timeout. token=0 keeps the app green and synced, which is exactly the state the
# desired-state check requires. max_seconds guards the point of the feature: the generous
# timeout below must NOT be waited out.
#
# Contrast with scenario_unvalidated_not_available above, which uses the app's REAL image name
# with an unreachable tag — that one must still wait, because the name is in the desired state.
#
# Depends on the lab leaving ARGO_REFRESH_APP at its default (true): the check is skipped
# without a refresh, and this scenario would then wait out its timeout and trip max_seconds.
scenario_image_not_part_of_app() {
  echo "task={\"app\":\"app2\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":120,\"images\":[{\"image\":\"ghcr.io/shini4i/not-in-this-app\",\"tag\":\"v1\"}]}"
  echo "token=0"
  echo "expect=is not part of application"
  echo "expect=List of images defined in the application:"
  echo "expect=${IMAGE}"
  echo "max_seconds=60"
  echo "setup=setup_image_not_part_of_app"
  return
}
# The desired-state check runs only against a Synced AND Healthy app, and the fixtures the earlier
# scenarios inject are repo-wide — so app2 is broken and pruned again alongside the app actually
# under test. Waiting here, rather than trusting the previous teardown, keeps max_seconds above
# honest: an app still mid-prune would skip the check and burn the full task timeout instead.
setup_image_not_part_of_app() {
  require_app_synced app2 60
  return
}

SCENARIOS=(
  scenario_bad_image
  scenario_unvalidated_not_available
  scenario_failed_presync_hook
  scenario_degraded_migration_job
  scenario_progressing_timeout
  scenario_image_not_part_of_app
)

# --- runner -----------------------------------------------------------------
for scenario in "${SCENARIOS[@]}"; do
  echo "=== ${scenario#scenario_} ==="
  spec="$($scenario)"
  task=$(sed -n 's/^task=//p' <<<"$spec")
  token=$(sed -n 's/^token=//p' <<<"$spec"); token="${token:-1}"
  setup=$(sed -n 's/^setup=//p' <<<"$spec")
  teardown=$(sed -n 's/^teardown=//p' <<<"$spec")
  max_seconds=$(sed -n 's/^max_seconds=//p' <<<"$spec")
  mapfile -t expects < <(sed -n 's/^expect=//p' <<<"$spec")

  [[ -n "$setup" ]] && { echo "  setup: $setup"; "$setup"; }

  started=$(date +%s)
  out=$(scenario_client "$task" "$token"); rc=$?
  elapsed=$(( $(date +%s) - started ))
  echo "  client exit=${rc} elapsed=${elapsed}s"
  # shellcheck disable=SC2001  # per-line prefix needs a regex anchor; ${//} can't do it
  echo "  client output: $(sed 's/^/    | /' <<<"$out")"

  [[ "$rc" -ne 0 ]] || bad "expected the client to exit non-zero, got ${rc}"
  for want in "${expects[@]}"; do
    if grep -qF -- "$want" <<<"$out"; then
      ok "client output contains «${want}»"
    else
      bad "client output missing «${want}»"
    fi
  done

  if [[ -n "$max_seconds" ]]; then
    if [[ "$elapsed" -le "$max_seconds" ]]; then
      ok "client returned in ${elapsed}s (<= ${max_seconds}s)"
    else
      bad "client took ${elapsed}s, expected <= ${max_seconds}s"
    fi
  fi

  [[ -n "$teardown" ]] && { echo "  teardown: $teardown"; "$teardown"; }
done

phase_end FAILURE-DIAGNOSTICS
