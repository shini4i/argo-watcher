#!/usr/bin/env bash
# Cases for lint-migrations.sh. That script's only job is to fail on a migration
# that breaks rollback, so a run against db/migrations — where everything passes —
# cannot tell a working check from one that accepts anything.
set -uo pipefail

LINT="$(dirname "$0")/lint-migrations.sh"
failures=0

# accept NAME BODY [FILE]  — the migration must pass the check
# reject NAME BODY [FILE]  — the migration must be rejected
assert() {
  local expect="$1" label="$2" body="$3" file="${4:-000042_case.up.sql}"
  local dir output actual
  dir=$(mktemp -d)
  printf '%s\n' "$body" > "$dir/$file"
  output=$("$LINT" "$dir" 2>&1)
  actual=$?
  rm -rf "$dir"

  if [ "$actual" -eq "$expect" ]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s (expected exit %s, got %s)\n%s\n' "$label" "$expect" "$actual" "$output"
    failures=$((failures + 1))
  fi
}

accept() { assert 0 "$1" "$2" "${3:-}"; }
reject() { assert 1 "$1" "$2" "${3:-}"; }

reject "bare DROP COLUMN"                'ALTER TABLE tasks DROP COLUMN foo;'
reject "DROP COLUMN without the keyword" 'ALTER TABLE tasks DROP legacy_field;'
reject "DROP TABLE"                      'DROP TABLE deploy_lock;'
reject "ALTER COLUMN ... TYPE"           'ALTER TABLE tasks ALTER COLUMN id TYPE uuid;'
reject "ALTER ... TYPE without keyword"  'ALTER TABLE tasks ALTER id TYPE uuid;'
reject "SET NOT NULL"                    'ALTER TABLE tasks ALTER COLUMN app SET NOT NULL;'
reject "RENAME COLUMN"                   'ALTER TABLE tasks RENAME COLUMN a TO b;'
reject "ADD COLUMN NOT NULL, no DEFAULT" 'ALTER TABLE tasks ADD COLUMN a TEXT NOT NULL;'
reject "ADD without the COLUMN keyword"  'ALTER TABLE tasks ADD bar TEXT NOT NULL;'

# Tabs are ordinary in hand-formatted SQL and must not hide a destructive change.
reject "tab-separated DROP COLUMN"       "$(printf 'ALTER TABLE tasks\n\tDROP\tCOLUMN foo;')"
reject "tab-separated ALTER COLUMN TYPE" "$(printf 'ALTER TABLE t ALTER\tCOLUMN x TYPE text;')"

reject "multi-column, second lacks DEFAULT" "ALTER TABLE tasks
  ADD COLUMN a TEXT NOT NULL DEFAULT '',
  ADD COLUMN b TEXT NOT NULL;"

accept "purely additive migration"       'CREATE TABLE foo (id INT PRIMARY KEY);'
accept "DROP INDEX only"                 'DROP INDEX IF EXISTS idx_foo;'
accept "DROP CONSTRAINT"                 'ALTER TABLE tasks DROP CONSTRAINT c;'
accept "DROP DEFAULT"                    'ALTER TABLE tasks ALTER COLUMN a DROP DEFAULT;'
accept "ADD COLUMN NOT NULL + DEFAULT"   "ALTER TABLE tasks ADD COLUMN a TEXT NOT NULL DEFAULT '';"
accept "ADD without keyword + DEFAULT"   "ALTER TABLE tasks ADD bar TEXT NOT NULL DEFAULT '';"
accept "ADD CONSTRAINT"                  'ALTER TABLE tasks ADD CONSTRAINT c CHECK (a IS NOT NULL);'
accept "ADD PRIMARY KEY"                 'ALTER TABLE tasks ADD PRIMARY KEY (id);'
accept "destructive text in a comment"   '-- we will DROP COLUMN foo one day
CREATE INDEX idx_x ON tasks (app);'

accept "tab-indented additive migration" "$(printf 'CREATE TABLE foo\n(\n\tid\tINT PRIMARY KEY\n);')"

accept "multi-column, both have DEFAULT" "ALTER TABLE tasks
  ADD COLUMN IF NOT EXISTS a TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS b INT NOT NULL DEFAULT 0;"

accept "DROP COLUMN with a recorded floor" 'ALTER TABLE tasks DROP COLUMN foo;
UPDATE schema_compatibility SET min_bundled_version = GREATEST(min_bundled_version, 10);'

# A floor of zero satisfies the pairing while protecting nothing.
reject "DROP COLUMN with a zero floor" 'ALTER TABLE tasks DROP COLUMN foo;
UPDATE schema_compatibility SET min_bundled_version = 0;'

accept "grandfathered file"              'ALTER TABLE tasks DROP COLUMN foo;' 000002_old.up.sql

# A misnamed file must not abort the loop and leave the run reporting success.
reject "no numeric prefix"               'CREATE TABLE foo (id INT);'      abc_x.up.sql
reject "no underscore separator"         'CREATE TABLE foo (id INT);'      000031.up.sql
reject "partly numeric prefix"           'CREATE TABLE foo (id INT);'      12ab_x.up.sql

# A destructive migration alongside a misnamed file must still be caught.
strays=$(mktemp -d)
printf 'CREATE TABLE a (id INT);\n' > "$strays/000031.up.sql"
printf 'DROP TABLE tasks;\n'        > "$strays/000032_z.up.sql"
if "$LINT" "$strays" >/dev/null 2>&1; then
  printf 'FAIL  destructive migration beside a misnamed file\n'
  failures=$((failures + 1))
else
  printf 'ok    destructive migration beside a misnamed file\n'
fi
rm -rf "$strays"

# An unmatched glob must not read as a pass.
empty=$(mktemp -d)
if "$LINT" "$empty" >/dev/null 2>&1; then
  printf 'FAIL  empty migrations directory\n'
  failures=$((failures + 1))
else
  printf 'ok    empty migrations directory\n'
fi
rm -rf "$empty"

if [ "$failures" -ne 0 ]; then
  printf '\n%s case(s) failed\n' "$failures"
  exit 1
fi

printf '\nAll lint-migrations cases pass\n'
