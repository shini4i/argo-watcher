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
#   setup / teardown       optional: names of functions run before/after the scenario
# The runner runs the client, captures its combined output + exit code, and greps.
#
# Usage: DEPLOY_TOKEN=... failure-diagnostics.sh
set -uo pipefail

NS_AW="${NS_AW:-argo-watcher}"
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
PORT="${PORT:-18095}"
IMAGE="${IMAGE:-traefik/whoami}"
GOOD_TAG="${GOOD_TAG:-v1.10.1}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"

# Build the client once; every scenario runs the same binary (deterministic, no
# per-invocation `go run` compile).
bin_dir="$(mktemp -d)"
CLIENT_BIN="${bin_dir}/aw-client"
( cd "$root" && go build -o "$CLIENT_BIN" ./cmd/client ) || { echo "failure-diagnostics: FAIL — client build failed" >&2; exit 1; }

# Register cleanup before starting the port-forward so an exit in the tiny window
# between the two can never orphan it.
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$bin_dir"' EXIT
kubectl -n "$NS_AW" port-forward svc/argo-watcher "${PORT}:80" >/dev/null 2>&1 &
for _ in $(seq 1 15); do curl -s -m3 -o /dev/null "localhost:${PORT}/healthz" && break; sleep 1; done

# --- helpers ----------------------------------------------------------------
# run_client <task-json> <use_token>: runs the client binary for the deploy
# described by the JSON. Prints the client's combined stdout+stderr; returns the
# client's exit code (0 = deployed, non-zero = failed/cancelled/etc.).
run_client() {
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
  env ARGO_WATCHER_URL="http://localhost:${PORT}" \
      IMAGES="$image" IMAGE_TAG="$tag" ARGO_APP="$app" \
      COMMIT_AUTHOR="$author" PROJECT_NAME="$project" \
      RETRY_INTERVAL="5s" TASK_TIMEOUT="$timeout" \
      "${token_env[@]+"${token_env[@]}"}" \
      "$CLIENT_BIN" 2>&1
  return
}

# restore_good_tag <app>: bump the app back to a pullable tag so the lab stays
# reusable. Best-effort — a restore hiccup must not fail the suite.
restore_good_tag() {
  local app="$1"
  run_client "{\"app\":\"${app}\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":120,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"${GOOD_TAG}\"}]}" 1 >/dev/null 2>&1 || true
  return
}

# --- scenarios --------------------------------------------------------------
# Committed image tag that cannot be pulled -> pods ImagePullBackOff. The cause lives ONLY on
# the Pod (resource tree), never in the app's top-level resources; the fix must surface it.
scenario_bad_image() {
  echo "task={\"app\":\"app1\",\"author\":\"e2e\",\"project\":\"lab\",\"timeout\":90,\"images\":[{\"image\":\"${IMAGE}\",\"tag\":\"v0.0.0-nopull\"}]}"
  echo "token=1"
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
  echo "token=1"
  echo "expect=Failed hooks:"
  echo "expect=PreSync Failed"
  echo "setup=setup_failed_presync_hook"
  echo "teardown=teardown_failed_presync_hook"
  return
}
setup_failed_presync_hook()    { "${here}/hook-fixture.sh" add app3; return; }
teardown_failed_presync_hook() { "${here}/hook-fixture.sh" remove app3; restore_good_tag app3; return; }

SCENARIOS=(
  scenario_bad_image
  scenario_unvalidated_not_available
  scenario_failed_presync_hook
)

# --- runner -----------------------------------------------------------------
overall=0
for scenario in "${SCENARIOS[@]}"; do
  echo "=== ${scenario#scenario_} ==="
  spec="$($scenario)"
  task=$(sed -n 's/^task=//p' <<<"$spec")
  token=$(sed -n 's/^token=//p' <<<"$spec"); token="${token:-1}"
  setup=$(sed -n 's/^setup=//p' <<<"$spec")
  teardown=$(sed -n 's/^teardown=//p' <<<"$spec")
  mapfile -t expects < <(sed -n 's/^expect=//p' <<<"$spec")

  [[ -n "$setup" ]] && { echo "  setup: $setup"; "$setup"; }

  out=$(run_client "$task" "$token"); rc=$?
  echo "  client exit=${rc}"
  # shellcheck disable=SC2001  # per-line prefix needs a regex anchor; ${//} can't do it
  echo "  client output: $(sed 's/^/    | /' <<<"$out")"

  ok=1
  [[ "$rc" -ne 0 ]] || { echo "  FAIL: expected the client to exit non-zero, got ${rc}"; ok=0; }
  for want in "${expects[@]}"; do
    if grep -qF -- "$want" <<<"$out"; then
      echo "  OK: client output contains «${want}»"
    else
      echo "  FAIL: client output missing «${want}»"; ok=0
    fi
  done

  [[ -n "$teardown" ]] && { echo "  teardown: $teardown"; "$teardown"; }
  [[ "$ok" -eq 1 ]] || overall=1
done

if [[ "$overall" -eq 0 ]]; then echo "FAILURE-DIAGNOSTICS: PASS"; else echo "FAILURE-DIAGNOSTICS: FAIL"; exit 1; fi
