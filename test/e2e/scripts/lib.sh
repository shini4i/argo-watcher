#!/usr/bin/env bash
# Shared helpers for the argo-watcher e2e lab phase scripts.
#
# Source it, never run it:
#   . "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
#
# Only helpers with more than one caller belong here; a phase's own domain
# assertions stay in the phase script, where the rationale for them is.
#
# Deliberately does NOT set shell options: the phases disagree (some run `set -e`
# and abort on the first problem, others `set -uo pipefail` and accumulate
# failures), and sourcing a library must not change the caller's mode. Everything
# below works under both.
#
# shellcheck disable=SC2034  # a library defines variables for the scripts sourcing it

# --- paths -------------------------------------------------------------------
E2E_SCRIPTS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(cd "${E2E_SCRIPTS}/.." && pwd)"
E2E_ROOT="$(cd "${E2E_DIR}/../.." && pwd)"

NS_AW="${NS_AW:-argo-watcher}"
NS_ARGOCD="${NS_ARGOCD:-argocd}"
NS_GITEA="${NS_GITEA:-gitea}"

# --- endpoints ---------------------------------------------------------------
# Fixed host ports, published by fixtures/nodeports/ and mapped into the node by
# kind-config.yaml. These survive a pod being replaced — which shutdown-drain,
# lockdown, batch-writeback and state-postgres all do deliberately — so those phases
# just wait for the service to answer again instead of re-forwarding.
AW_URL="${AW_URL:-http://localhost:30080}"
AW_API="${AW_URL}/api/v1"
AW_WS_URL="${AW_WS_URL:-ws://localhost:30080/ws}"
WHT_URL="${WHT_URL:-http://localhost:30081}"
GITEA_URL="${GITEA_URL:-http://localhost:30300}"
# Self-signed cert; callers pass -k.
ARGOCD_URL="${ARGOCD_URL:-https://localhost:30443}"

# Throwaway lab credentials, in vars rather than an inline user:pass@ URL literal.
GITEA_ADMIN="${GITEA_ADMIN:-gitea_admin}"
GITEA_PW="${GITEA_PW:-gitea_admin_pw1}"
GITEA_ORG="${GITEA_ORG:-e2e}"
GITEA_REPO="${GITEA_REPO:-gitops}"
GITEA_API="${GITEA_URL}/api/v1"
GITOPS_REPO_URL="${GITOPS_REPO_URL:-http://${GITEA_ADMIN}:${GITEA_PW}@localhost:30300/${GITEA_ORG}/${GITEA_REPO}.git}"

# Only validated (authenticated) tasks trigger the updater push, so a phase asserting
# a write-back must send this. Must match the secret mint-argo-token.sh writes.
DEPLOY_TOKEN="${DEPLOY_TOKEN:-e2e-deploy-token}"
IMAGE="${IMAGE:-traefik/whoami}"

# --- assertions --------------------------------------------------------------
E2E_FAILS=0

ok()   { echo "  OK   $*"; }
bad()  { echo "  FAIL $*"; E2E_FAILS=$((E2E_FAILS + 1)); }
note() { echo "  NOTE $*"; }

# phase_end <PHASE-NAME>: print the verdict and exit 1 if any bad() fired.
phase_end() {
  if [[ "$E2E_FAILS" -eq 0 ]]; then
    echo "${1}: PASS"
    exit 0
  fi
  echo "${1}: FAIL (${E2E_FAILS} failed assertion(s))"
  exit 1
}

# die <message...>: abort the phase immediately. For preconditions a phase cannot
# proceed without (a fixture never synced, a build failed) as opposed to a failed
# assertion, which is bad() + phase_end.
die() { echo "FAIL: $*" >&2; exit 1; }

# --- polling -----------------------------------------------------------------
# retry <attempts> <delay-seconds> <cmd...>: run cmd until it succeeds. Returns 0
# on the first success, 1 once every attempt has failed. cmd's own output is left
# alone — redirect it at the call site if it is noisy.
#
# The locals are underscore-prefixed because cmd runs in THIS shell and may well
# touch a variable of its own called `attempts` or `delay`. Without the prefix such
# a name would resolve to retry's local: a cmd incrementing its own `attempts`
# counter would grow this loop's bound on every pass and never terminate.
retry() {
  local _attempts="$1" _delay="$2" _i
  shift 2
  for ((_i = 1; _i <= _attempts; _i++)); do
    "$@" && return 0
    [[ "$_i" -lt "$_attempts" ]] && sleep "$_delay"
  done
  return 1
}

# wait_service [attempts]: block until argo-watcher answers on its HTTP port.
#
# Deliberately checks only that a response arrives, NOT that it is 2xx: /healthz
# returns 503 while ArgoCD is unreachable (handlers.go healthz), which is a state
# the argocd-unreachable phase induces on purpose and the shutdown-drain phase can
# observe on a fresh pod. A `curl -f` here would hang in exactly those phases.
wait_service() {
  retry "${1:-30}" 2 curl -s -m 3 -o /dev/null "${AW_URL}/healthz"
}

# wait_url <url> [attempts]: block until <url> answers at all (see wait_service on
# why the status code is not checked).
wait_url() {
  retry "${2:-15}" 2 curl -s -m 3 -o /dev/null "$1"
}

# wait_app <app> <want> [attempts]: poll an Argo Application until its
# "<sync>/<health>" state equals <want> (e.g. "Synced/Healthy") — or, when <want>
# has no slash, until its health alone equals it (e.g. "Healthy"). Leaves the last
# observed state in APP_STATE so the caller can report what it actually saw.
wait_app() {
  local app="$1" want="$2" attempts="${3:-40}" path i
  if [[ "$want" == */* ]]; then
    path='{.status.sync.status}/{.status.health.status}'
  else
    path='{.status.health.status}'
  fi
  for ((i = 1; i <= attempts; i++)); do
    APP_STATE=$(kubectl -n "$NS_ARGOCD" get application "$app" -o jsonpath="$path" 2>/dev/null || true)
    [[ "$APP_STATE" == "$want" ]] && return 0
    sleep 5
  done
  return 1
}

# require_app_synced <app> [attempts]: wait_app for Synced/Healthy, aborting with the
# last observed state if it never gets there. Deploy phases open with this so they
# deploy from a known-good baseline.
require_app_synced() {
  wait_app "$1" "Synced/Healthy" "${2:-40}" \
    || die "${1} never reached Synced/Healthy (last: ${APP_STATE:-unknown})"
}

# wait_ws <file> <message> [attempts]: wait for a wsprobe capture file to contain
# `MSG <message>`. The lockdown watcher polls every 5s, so the default ~30s window
# covers a broadcast triggered just after the previous tick.
wait_ws() {
  retry "${3:-6}" 5 grep -q "^MSG ${2}\$" "$1"
}

# wait_ws_open <file> [attempts]: wait for a wsprobe to report its connection is
# established. Every phase asserting a broadcast must do this BEFORE triggering it:
# a one-shot broadcast is missed entirely if the handshake is still in flight, and
# a fixed sleep raced it on a loaded host.
wait_ws_open() {
  retry "${2:-20}" 1 grep -q '^OPEN$' "$1"
}

# --- HTTP --------------------------------------------------------------------
# req <method> <url> [curl-args...]: perform one request, setting CODE (HTTP
# status), BODY (response body) and TIME (total seconds) for the caller to assert
# on. The status and timing are appended on their own lines and split off from the
# end, so a body containing newlines survives intact.
req() {
  local method="$1" url="$2" out
  shift 2
  out=$(curl -s -m "${REQ_TIMEOUT:-10}" -w $'\n%{http_code}\n%{time_total}' \
    -X "$method" "$@" "$url")
  TIME="${out##*$'\n'}"; out="${out%$'\n'*}"
  CODE="${out##*$'\n'}"; BODY="${out%$'\n'*}"
}

# task_json <app> <tag> [author] [project] [image]: a POST /tasks payload.
task_json() {
  printf '{"app":"%s","author":"%s","project":"%s","images":[{"image":"%s","tag":"%s"}]}' \
    "$1" "${3:-e2e}" "${4:-lab}" "${5:-$IMAGE}" "$2"
}

# post_task <json> [curl-args...]: POST a deploy task, setting CODE/BODY/TIME.
# Tokenless by default — an unauthenticated task is accepted (202) but skips
# write-back, so it has no lasting side effect. Pass
# -H "ARGO_WATCHER_DEPLOY_TOKEN: ${DEPLOY_TOKEN}" for a validated one.
post_task() {
  local json="$1"
  shift
  req POST "${AW_API}/tasks" -H 'Content-Type: application/json' -d "$json" "$@"
}

# metric_sum <metric> [metrics-text]: sum the value column across every series of
# <metric>, scraping /metrics if no text is given. The `[ {]` guard anchors on the
# exact metric name, so `processed_deployments` never captures
# `processed_deployments_created` and a histogram's `_count` never pulls in its
# `_bucket` / `_sum` siblings.
metric_sum() {
  local metric="$1" text="${2-}"
  [[ -n "$text" ]] || text="$(curl -s -m 10 "${AW_URL}/metrics")"
  awk -v k="^${metric}[ {]" '$0 ~ k {s+=$NF} END{print s+0}' <<<"$text"
}

# metric_raw <metric> [metrics-text]: the value of a single unlabelled series, or
# EMPTY when the metric is absent from the scrape.
#
# Use this, not metric_sum, for any gauge whose gate is "must be 0": metric_sum
# reports a missing metric as 0, so an unregistered or renamed gauge would satisfy
# `== "0"` and the gate would pass vacuously — which is exactly the regression these
# gauges exist to catch. An empty result fails the comparison instead.
metric_raw() {
  local metric="$1" text="${2-}"
  [[ -n "$text" ]] || text="$(curl -s -m 10 "${AW_URL}/metrics")"
  awk -v k="^${metric} " '$0 ~ k {print $NF}' <<<"$text"
}

# --- Go binaries -------------------------------------------------------------
# build_bin <dir> <pkg>: build a Go binary from the repo root into <dir>, setting
# BIN to its path. Phases run a prebuilt binary rather than `go run` so a
# per-invocation compile never lands inside a timing-sensitive assertion (the
# supersession race needs sub-second submission ordering). The caller owns <dir>
# and its cleanup.
#
# Sets a variable instead of echoing the path so a build failure is visible to `||`
# at the call site: inside $(...) it would only abort the subshell, and the phases
# that run without `set -e` would carry on with an empty path.
build_bin() {
  local _dir="$1" _pkg="$2"
  BIN="${_dir}/$(basename "$_pkg")"
  ( cd "$E2E_ROOT" && go build -o "$BIN" "$_pkg" )
}

# build_client <dir>: build the real cmd/client binary, setting CLIENT_BIN for
# run_client. The tool users actually run, so the deploy phases drive it rather
# than a hand-rolled HTTP call.
build_client() {
  build_bin "$1" ./cmd/client || return 1
  CLIENT_BIN="$BIN"
}

# run_client <app> <tag> [extra env KEY=VAL...]: run the real cmd/client binary for
# one deploy, blocking until the task reaches a terminal status, with combined
# stdout+stderr on stdout. The client's exit code IS the assertion: 0 = "deployed",
# non-zero = anything else. CLIENT_BIN must point at a built binary (build_bin).
#
# Callers pass auth explicitly, because which strategy is under test differs by
# phase: ARGO_WATCHER_DEPLOY_TOKEN="$DEPLOY_TOKEN" (write-back enabled),
# BEARER_TOKEN="$jwt" (the JWT path), or nothing (unvalidated, no write-back).
# IMAGES / COMMIT_AUTHOR / PROJECT_NAME / RETRY_INTERVAL / TASK_TIMEOUT can be
# overridden the same way, since env args later on the line win.
run_client() {
  local app="$1" tag="$2"
  shift 2
  env ARGO_WATCHER_URL="$AW_URL" \
      IMAGES="$IMAGE" IMAGE_TAG="$tag" ARGO_APP="$app" \
      COMMIT_AUTHOR="e2e" PROJECT_NAME="lab" \
      RETRY_INTERVAL="5s" TASK_TIMEOUT="180" \
      "$@" "$CLIENT_BIN" 2>&1
}

# --- gitops repo -------------------------------------------------------------
# gitops_clone <dir>: clone the lab's shared gitops repo (every fixture app renders
# from it, which is what makes their write-back pushes contend) into <dir>.
gitops_clone() {
  git clone -q "$GITOPS_REPO_URL" "$1" || die "gitops clone failed"
}

# override_param <file> <param>: read one helm parameter's value out of an
# .argocd-source-*.yaml write-back override. The file is small controlled YAML
# (helm.parameters: [{name, value, forceString}]); awk keeps this yq-free, since yq
# is not installed on GitHub runners. After the <param> name line, take the value
# on the next `value:` line, stripping quotes. Echoes nothing when the parameter is
# absent — callers must tell that apart from a wrong value, or an unwritten
# override reads as a wrong-tag failure.
#
# The name is compared as an exact string, not a regex: parameter names contain
# dots (app.image.tag), and as a pattern those match any character — so
# `appXimageYtag` would match. Escaping them is not an option either, because awk's
# -v processes escape sequences and turns `\.` back into a plain dot.
override_param() {
  awk -v want="$2" '
    # A name line, e.g. `    - name: app.image.tag`; the value is on a later line.
    /(^|[[:space:]])name:[[:space:]]/ {
      n = $NF; gsub(/"/, "", n)
      if (n == want) { f = 1 }
    }
    f && /value:/ { v = $NF; gsub(/"/, "", v); print v; exit }
  ' "$1"
}

# other_tag <app>: echo a pullable tag DIFFERENT from the one <app> currently runs,
# so deploying it forces a real write-back. An unchanged tag is byte-compared and
# skipped (#472), which would make a write-back assertion pass vacuously — and
# earlier phases leave apps on varying tags, so this cannot be hardcoded.
other_tag() {
  local cur
  cur=$(kubectl -n "$NS_ARGOCD" get application "$1" -o jsonpath='{.status.summary.images}' 2>/dev/null || true)
  if [[ "$cur" == *v1.10.2* ]]; then echo "v1.10.1"; else echo "v1.10.2"; fi
}

# --- helm --------------------------------------------------------------------
# extra_envs_index <values-file>: the index of the next free extraEnvs entry, so a
# phase can append one with `--set extraEnvs[N]...`. Counted from the values FILE,
# not the live release, so it does not depend on release state. The awk scopes the
# count to the extraEnvs block (between "extraEnvs:" and the next top-level key),
# so a same-indented "- name:" added to another section cannot silently shift it.
extra_envs_index() {
  awk '
    /^extraEnvs:/ { f = 1; next }
    f && /^[^[:space:]#]/ { f = 0 }
    f && /^  - name:/ { c++ }
    END { print c + 0 }
  ' "$1"
}

# helm_apply_aw [extra --set args...]: reconfigure the LIVE argo-watcher release
# from values/argo-watcher.yaml plus the given args, then wait for the rollout.
# Used by the phases that toggle a server-global env var for their own duration and
# revert before returning (a global cannot be set in the shared install without
# affecting every other phase).
#
# --reset-values makes each apply deterministic from the values file + these args
# alone: without it helm carries a prior `--set extraEnvs[N]` forward, so the
# revert (values file only) would NOT drop the injected variable.
#
# Requires AW_CHART_REPO and AW_CHART_VERSION (passed from the Taskfile, which pins
# the chart version).
helm_apply_aw() {
  helm upgrade --install argo-watcher argo-watcher \
    --repo "${AW_CHART_REPO:?AW_CHART_REPO is required}" \
    --version "${AW_CHART_VERSION:?AW_CHART_VERSION is required}" \
    -n "$NS_AW" -f "${E2E_DIR}/values/argo-watcher.yaml" --reset-values \
    --set image.tag=race "$@" >/dev/null
  kubectl -n "$NS_AW" rollout status statefulset/argo-watcher --timeout=180s >/dev/null
}
