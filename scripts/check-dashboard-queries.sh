#!/usr/bin/env bash
# Fails when the "Clients Without Auth" query drifts between the dashboard, its promtool
# test and the copy in the observability docs, so the tested expression is the shipped
# one. Usage: check-dashboard-queries.sh [DASHBOARD] [TEST_FILE] [DOCS]. Tested by
# check-dashboard-queries.bats.
set -euo pipefail

DASHBOARD="${1:-monitoring/grafana/dashboards/argo-watcher.json}"
TEST_FILE="${2:-monitoring/prometheus/tests/unauthenticated-reads.test.yml}"
DOCS="${3:-docs/operations/observability.md}"

PANEL_TITLE="Clients Without Auth (selected range)"

expr="$(jq -er --arg title "$PANEL_TITLE" \
  '.panels[] | select(.title == $title) | .targets[0].expr' "$DASHBOARD")" || {
  echo "no panel titled \"$PANEL_TITLE\" in $DASHBOARD" >&2
  exit 1
}

# grep -F reads a multi-line pattern as alternatives, so a match would prove nothing.
if [[ "$expr" == *$'\n'* ]]; then
  echo "the panel query must be a single line, and the panel title unique" >&2
  exit 1
fi

status=0

# The test file pins the expression with $__range set to 1h, exactly as written.
if ! grep -qF -- "${expr//\$__range/1h}" "$TEST_FILE"; then
  echo "$TEST_FILE does not test the dashboard query" >&2
  status=1
fi

# The docs spread the query over several lines with a 1d window, so compare with all
# whitespace removed.
squashed_expr="$(printf '%s' "${expr//\$__range/1d}" | tr -d '[:space:]')"
if ! tr -d '[:space:]' < "$DOCS" | grep -qF -- "$squashed_expr"; then
  echo "$DOCS does not carry the dashboard query" >&2
  status=1
fi

exit "$status"
