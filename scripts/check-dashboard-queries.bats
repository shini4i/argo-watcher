#!/usr/bin/env bats
# Cases for check-dashboard-queries.sh. On the repository's own files the check always
# passes, which cannot tell a working check from one that accepts anything, so each
# rejection is driven from a drifted copy and asserts the reason.
#
#   task test-dashboard     # or: bats scripts/check-dashboard-queries.bats

bats_require_minimum_version 1.5.0

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert

  CHECK="${BATS_TEST_DIRNAME}/check-dashboard-queries.sh"
  ROOT="${BATS_TEST_DIRNAME}/.."
  WORK="$(mktemp -d)"
  cp "$ROOT/monitoring/grafana/dashboards/argo-watcher.json" "$WORK/dashboard.json"
  cp "$ROOT/monitoring/prometheus/tests/unauthenticated-reads.test.yml" "$WORK/test.yml"
  cp "$ROOT/docs/operations/observability.md" "$WORK/docs.md"
}

teardown() {
  rm -rf "$WORK"
}

check() {
  run "$CHECK" "$WORK/dashboard.json" "$WORK/test.yml" "$WORK/docs.md"
}

@test "accepts the repository's own files" {
  check
  assert_success
}

@test "rejects a test file that tests a different query" {
  sed -i 's/increase(unauthenticated_reads\[1h\])/rate(unauthenticated_reads[1h])/' "$WORK/test.yml"
  check
  assert_failure
  assert_output --partial "does not test the dashboard query"
}

@test "rejects docs carrying a different query" {
  sed -i 's/or increase(unauthenticated_reads\[1d\])/or rate(unauthenticated_reads[1d])/' "$WORK/docs.md"
  check
  assert_failure
  assert_output --partial "does not carry the dashboard query"
}

@test "rejects a dashboard changed without its test and docs" {
  sed -i 's/> 0"/> 1"/' "$WORK/dashboard.json"
  check
  assert_failure
  assert_output --partial "does not test the dashboard query"
  assert_output --partial "does not carry the dashboard query"
}

@test "rejects a panel query spread over several lines" {
  sed -i 's/ or increase(unauthenticated_reads\[\$__range\])/\\n or increase(unauthenticated_reads[$__range])/' "$WORK/dashboard.json"
  check
  assert_failure
  assert_output --partial "must be a single line"
}

@test "rejects a dashboard without the panel" {
  sed -i 's/Clients Without Auth (selected range)/Renamed/' "$WORK/dashboard.json"
  check
  assert_failure
  assert_output --partial "no panel titled"
}
