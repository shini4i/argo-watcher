#!/usr/bin/env bash
# Enforces the forward-only migration rule that makes rollback safe: a release
# rolled back runs against the schema its successor left behind, so a migration
# must not remove anything an older build still reads unless it also raises the
# compatibility floor in schema_compatibility. Tested by lint-migrations_test.sh.
set -euo pipefail

MIGRATIONS_DIR="${1:-db/migrations}"

# Migrations up to this version predate the rule and cannot be changed. 000003
# rewrote column types; 000004 drops two indexes, which costs performance only.
GRANDFATHERED_THROUGH=3

status=0
checked=0

for file in "$MIGRATIONS_DIR"/*.up.sql; do
  [[ -e "$file" ]] || continue

  name="${file##*/}"

  # Validated before the arithmetic below, which would otherwise abort the loop
  # on a stray file and leave the run reporting success without checking the rest.
  prefix="${name%%_*}"
  case "$prefix" in
    '' | *[!0-9]*)
      echo "===> ERROR: $name does not follow the NNNNNN_description.up.sql convention" >&2
      status=1
      continue
      ;;
  esac

  checked=$((checked + 1))
  version=$((10#$prefix))
  [[ "$version" -le "$GRANDFATHERED_THROUGH" ]] && continue

  # Every pattern below matches on literal spaces, so all whitespace is flattened
  # first; leaving tabs in place would let tab-formatted SQL slip past. Line
  # comments go before that, block comments after, since they span lines. Both must
  # go, or a floor update that is only commented out would satisfy the pairing.
  sql=$(sed 's/--.*$//' "$file" | tr '[:space:]' ' ' | tr -s ' ' \
    | sed -E 's|/\*[^*]*\*+([^/*][^*]*\*+)*/| |g' | tr '[:lower:]' '[:upper:]')

  findings=$(printf '%s' "$sql" | awk '
    {
      sql = $0

      if (sql ~ /RENAME/)             print "RENAME"
      if (sql ~ /SET[ ]+NOT[ ]+NULL/) print "SET NOT NULL"

      # PostgreSQL makes the COLUMN keyword optional after DROP, so the clause is
      # split and its first word inspected. Anything that is not a known
      # non-destructive object is treated as a column drop.
      n = split(sql, drops, /DROP[ ]+/)
      for (i = 2; i <= n; i++) {
        split(drops[i], word, " ")
        keyword = word[1]
        gsub(/[^A-Z]/, "", keyword)
        if (keyword == "TABLE") { print "DROP TABLE"; continue }
        if (keyword ~ /^(INDEX|CONSTRAINT|DEFAULT|NOT|TRIGGER|VIEW|SEQUENCE|SCHEMA|FUNCTION|POLICY)$/) continue
        print "DROP COLUMN"
      }

      # COLUMN is optional after ALTER too. The ALTER TABLE clause that opens the
      # statement names the table, not a column, so it is skipped.
      n = split(sql, alters, /ALTER[ ]+/)
      for (i = 2; i <= n; i++) {
        split(alters[i], head, ",")
        if (head[1] ~ /^TABLE/) continue
        if (head[1] ~ /[ ]TYPE[ ]/) print "ALTER COLUMN ... TYPE"
      }

      # A NOT NULL column with no DEFAULT breaks the previous release inserts,
      # which do not supply it. Each clause is read up to its own comma so a later
      # column DEFAULT cannot mask an earlier omission; constraints are skipped.
      n = split(sql, clauses, /ADD[ ]+(COLUMN[ ]+)?/)
      for (i = 2; i <= n; i++) {
        split(clauses[i], head, ",")
        if (head[1] ~ /^(CONSTRAINT|PRIMARY|FOREIGN|UNIQUE|CHECK|EXCLUDE)/) continue
        if (head[1] ~ /NOT[ ]+NULL/ && head[1] !~ /DEFAULT/)
          print "ADD COLUMN ... NOT NULL without DEFAULT"
      }
    }
  ' | sort -u)

  [[ -z "$findings" ]] && continue

  # The floor tells an older build to refuse to start rather than fail later on a
  # column that is gone. It must name a real version: setting it to 0 would
  # satisfy the pairing while protecting nothing.
  if printf '%s' "$sql" | grep -qE 'UPDATE +SCHEMA_COMPATIBILITY[^;]*MIN_BUNDLED_VERSION[^;]*[1-9]'; then
    continue
  fi

  status=1
  echo "===> ERROR: $name changes the schema in a way an older build cannot survive:"
  while IFS= read -r finding; do
    echo "         - $finding"
  done <<< "$findings"
  echo "         Either keep the change additive (a NOT NULL column needs a DEFAULT),"
  echo "         or record the floor in the same file:"
  echo "           UPDATE schema_compatibility"
  echo "              SET min_bundled_version = GREATEST(min_bundled_version, <version>);"
  echo "         where <version> is the migration at which the code stopped using it."
done

# A renamed directory, or the task run from elsewhere, would otherwise leave the
# glob unmatched and report a pass without having checked anything.
if [[ "$checked" -eq 0 ]]; then
  echo "===> ERROR: no *.up.sql found in $MIGRATIONS_DIR" >&2
  exit 1
fi

if [[ "$status" -eq 0 ]]; then
  echo "===> Checked $checked migrations; all forward-only"
fi

exit "$status"
