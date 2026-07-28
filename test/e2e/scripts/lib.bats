#!/usr/bin/env bats
# Unit tests for the pure text-processing helpers in lib.sh — the awk/parameter
# expansion logic whose failure mode is silent and wrong (a parse that returns
# nothing reads as "assertion failed", a metric filter that over-matches inflates a
# gate) rather than a loud error.
#
# Cluster-dependent helpers (wait_app, run_client, helm_apply_aw, ...) are not
# covered here: they are only meaningful against the real lab, where the phases that
# call them are the test.
#
#   task -d test/e2e lint     # or: bats test/e2e/scripts/lib.bats

# `run !` and `run -N` need 1.5.0+.
bats_require_minimum_version 1.5.0

setup() {
  # bats_load_library, not load: BATS_LIB_PATH is a colon-separated SEARCH path, so
  # treating it as a single directory breaks as soon as it holds more than one entry.
  bats_load_library bats-support
  bats_load_library bats-assert
  # shellcheck source=./lib.sh
  . "${BATS_TEST_DIRNAME}/lib.sh"
}

# --- override_param ----------------------------------------------------------

override_fixture() {
  cat >"${BATS_TEST_TMPDIR}/o.yaml" <<'YAML'
helm:
  parameters:
    - name: app.image.tag
      value: "v1.10.3"
      forceString: true
    - name: app.proxyTag
      value: latest
      forceString: true
YAML
}

@test "override_param reads a quoted value" {
  override_fixture
  run override_param "${BATS_TEST_TMPDIR}/o.yaml" app.image.tag
  assert_output "v1.10.3"
}

@test "override_param reads an unquoted value" {
  override_fixture
  run override_param "${BATS_TEST_TMPDIR}/o.yaml" app.proxyTag
  assert_output "latest"
}

@test "override_param yields nothing for an absent parameter" {
  override_fixture
  run override_param "${BATS_TEST_TMPDIR}/o.yaml" app.nope
  assert_output ""
}

@test "override_param treats dots as literal, not regex wildcards" {
  # Unescaped, "app.image.tag" as a pattern also matches "appXimageYtag".
  cat >"${BATS_TEST_TMPDIR}/dots.yaml" <<'YAML'
helm:
  parameters:
    - name: appXimageYtag
      value: "wrong"
    - name: app.image.tag
      value: "right"
YAML
  run override_param "${BATS_TEST_TMPDIR}/dots.yaml" app.image.tag
  assert_output "right"
}

@test "override_param does not match a name by its prefix" {
  # "app.image" must not be satisfied by the "app.image.tag" entry above it.
  cat >"${BATS_TEST_TMPDIR}/prefix.yaml" <<'YAML'
helm:
  parameters:
    - name: app.image.tag
      value: "tag-entry"
    - name: app.image
      value: "image-entry"
YAML
  run override_param "${BATS_TEST_TMPDIR}/prefix.yaml" app.image
  assert_output "image-entry"
}

# --- extra_envs_index --------------------------------------------------------

@test "extra_envs_index counts the entries in the block" {
  cat >"${BATS_TEST_TMPDIR}/v.yaml" <<'YAML'
logLevel: debug

extraEnvs:
  - name: JWT_SECRET
    value: "s"
  - name: ARGO_URL_ALIAS
    value: "u"

postgres:
  enabled: false
YAML
  run extra_envs_index "${BATS_TEST_TMPDIR}/v.yaml"
  assert_output "2"
}

@test "extra_envs_index stops at the next top-level key" {
  # A same-indented "- name:" in another section would otherwise shift the index and
  # make a --set overwrite a real entry instead of appending.
  cat >"${BATS_TEST_TMPDIR}/v.yaml" <<'YAML'
extraEnvs:
  - name: JWT_SECRET
    value: "s"

sidecars:
  - name: not-an-env
    image: busybox
YAML
  run extra_envs_index "${BATS_TEST_TMPDIR}/v.yaml"
  assert_output "1"
}

@test "extra_envs_index yields 0 with no extraEnvs block" {
  echo "logLevel: debug" >"${BATS_TEST_TMPDIR}/v.yaml"
  run extra_envs_index "${BATS_TEST_TMPDIR}/v.yaml"
  assert_output "0"
}

# --- metric_sum --------------------------------------------------------------

metrics_fixture() {
  cat <<'PROM'
# HELP failed_deployment Failed deployments
failed_deployment{app="app1"} 2
failed_deployment{app="app2"} 3
processed_deployments 7
processed_deployments_created 1.7e+09
argocd_unavailable 0
gitops_writeback_duration_seconds_bucket{le="0.5"} 11
gitops_writeback_duration_seconds_sum 4.2
gitops_writeback_duration_seconds_count 9
in_progress_tasks 0
PROM
}

@test "metric_sum sums labelled series" {
  run metric_sum failed_deployment "$(metrics_fixture)"
  assert_output "5"
}

@test "metric_sum reads an unlabelled series" {
  run metric_sum processed_deployments "$(metrics_fixture)"
  assert_output "7"
}

@test "metric_sum preserves a zero value" {
  run metric_sum argocd_unavailable "$(metrics_fixture)"
  assert_output "0"
}

@test "metric_sum yields 0 for an absent metric" {
  run metric_sum nonexistent_metric "$(metrics_fixture)"
  assert_output "0"
}

@test "metric_sum does not capture the _created sibling" {
  # A bare prefix match would fold processed_deployments_created (a unix timestamp)
  # into processed_deployments.
  run metric_sum processed_deployments "$(metrics_fixture)"
  assert_output "7"
}

@test "metric_sum histogram _count excludes _bucket and _sum" {
  run metric_sum gitops_writeback_duration_seconds_count "$(metrics_fixture)"
  assert_output "9"
}

# --- metric_raw ---------------------------------------------------------------
# The gauges gated on "must be 0" have to distinguish 0 from absent, which is the one
# thing metric_sum cannot do: it reports a missing metric as 0, so an unregistered or
# renamed gauge would satisfy the gate vacuously.

@test "metric_raw reads a present gauge" {
  run metric_raw argocd_unavailable "$(metrics_fixture)"
  assert_output "0"
}

@test "metric_raw yields EMPTY for an absent gauge, unlike metric_sum" {
  run metric_raw definitely_absent_gauge "$(metrics_fixture)"
  assert_output ""
  # The contrast is the whole point of having both helpers.
  run metric_sum definitely_absent_gauge "$(metrics_fixture)"
  assert_output "0"
}

@test "metric_raw does not match a longer metric name by prefix" {
  # The trailing space in the pattern is what keeps in_progress_tasks from also
  # matching an in_progress_tasks_total, and argocd_unavailable from a _created.
  run metric_raw processed_deployments "$(metrics_fixture)"
  assert_output "7"
}

# --- task_json ---------------------------------------------------------------

@test "task_json builds a payload with defaults" {
  json="$(task_json app1 v1.2.3)"
  assert_equal "$(jq -r '.app' <<<"$json")" "app1"
  assert_equal "$(jq -r '.author' <<<"$json")" "e2e"
  assert_equal "$(jq -r '.project' <<<"$json")" "lab"
  assert_equal "$(jq -r '.images[0].tag' <<<"$json")" "v1.2.3"
  assert_equal "$(jq -r '.images[0].image' <<<"$json")" "traefik/whoami"
}

@test "task_json honours explicit author, project and image" {
  json="$(task_json app9 v9 bob proj custom/img)"
  assert_equal "$(jq -r '.author' <<<"$json")" "bob"
  assert_equal "$(jq -r '.project' <<<"$json")" "proj"
  assert_equal "$(jq -r '.images[0].image' <<<"$json")" "custom/img"
}

# --- retry -------------------------------------------------------------------
# These counters are deliberately named `attempts` and `delay` — the same names
# retry takes as parameters. A command retry runs executes in the same shell, so if
# retry's locals were not underscore-prefixed, `never` below would increment retry's
# own loop bound and the loop would never terminate.

@test "retry succeeds on the third attempt" {
  attempts=0
  flaky() { attempts=$((attempts + 1)); [[ "$attempts" -ge 3 ]]; }
  retry 5 0 flaky
  assert_equal "$attempts" "3"
}

@test "retry returns 1 once exhausted, having run exactly <attempts> times" {
  attempts=0
  delay=0
  never() { attempts=$((attempts + 1)); return 1; }
  run ! retry 4 0 never
  # `run` uses a subshell, so re-run in this shell to observe the counter.
  attempts=0
  retry 4 0 never || true
  assert_equal "$attempts" "4"
  assert_equal "$delay" "0"
}

@test "retry stops at the first success" {
  attempts=0
  always() { attempts=$((attempts + 1)); return 0; }
  retry 3 0 always
  assert_equal "$attempts" "1"
}

# --- assertion accumulator ---------------------------------------------------

@test "phase_end exits 0 when nothing failed" {
  run bash -c '. "'"${BATS_TEST_DIRNAME}"'/lib.sh"; ok fine; phase_end SAMPLE'
  assert_success
  assert_output --partial "SAMPLE: PASS"
}

@test "phase_end exits 1 after a bad() and counts every failure" {
  run bash -c '. "'"${BATS_TEST_DIRNAME}"'/lib.sh"; bad one; bad two; phase_end SAMPLE'
  assert_failure
  assert_output --partial "SAMPLE: FAIL (2 failed assertion(s))"
}
