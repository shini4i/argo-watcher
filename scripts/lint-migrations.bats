#!/usr/bin/env bats
# Cases for lint-migrations.sh. That script's only job is to reject a migration that
# breaks rollback, so running it against db/migrations — where everything passes —
# cannot tell a working check from one that accepts anything.
#
# Each rejection asserts the reason, not just the exit status: a check that fails for
# the wrong reason is as broken as one that does not fail at all.
#
#   task lint-migrations     # or: bats scripts/lint-migrations.bats

bats_require_minimum_version 1.5.0

setup() {
  # bats_load_library, not load: BATS_LIB_PATH is a colon-separated SEARCH path.
  bats_load_library bats-support
  bats_load_library bats-assert

  LINT="${BATS_TEST_DIRNAME}/lint-migrations.sh"
  MIGRATIONS="$(mktemp -d)"
}

teardown() {
  rm -rf "$MIGRATIONS"
}

# migration BODY [FILENAME] — write one migration into this case's directory.
migration() {
  printf '%s\n' "$1" > "$MIGRATIONS/${2:-000042_case.up.sql}"
}

@test "rejects a bare DROP COLUMN" {
  migration 'ALTER TABLE tasks DROP COLUMN foo;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "rejects a DROP without the optional COLUMN keyword" {
  migration 'ALTER TABLE tasks DROP legacy_field;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "rejects a DROP TABLE" {
  migration 'DROP TABLE deploy_lock;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP TABLE'
}

@test "rejects an ALTER COLUMN ... TYPE" {
  migration 'ALTER TABLE tasks ALTER COLUMN id TYPE uuid;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'ALTER COLUMN ... TYPE'
}

@test "rejects an ALTER ... TYPE without the optional COLUMN keyword" {
  migration 'ALTER TABLE tasks ALTER id TYPE uuid;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'ALTER COLUMN ... TYPE'
}

@test "rejects a SET NOT NULL" {
  migration 'ALTER TABLE tasks ALTER COLUMN app SET NOT NULL;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'SET NOT NULL'
}

@test "rejects a RENAME" {
  migration 'ALTER TABLE tasks RENAME COLUMN a TO b;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'RENAME'
}

@test "rejects an ADD COLUMN NOT NULL with no DEFAULT" {
  migration 'ALTER TABLE tasks ADD COLUMN a TEXT NOT NULL;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'NOT NULL without DEFAULT'
}

@test "rejects an ADD NOT NULL without the optional COLUMN keyword" {
  migration 'ALTER TABLE tasks ADD bar TEXT NOT NULL;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'NOT NULL without DEFAULT'
}

@test "rejects a multi-column ADD where only a later column has a DEFAULT" {
  migration "ALTER TABLE tasks
  ADD COLUMN a TEXT NOT NULL DEFAULT '',
  ADD COLUMN b TEXT NOT NULL;"
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'NOT NULL without DEFAULT'
}

# Tabs are ordinary in hand-formatted SQL. The patterns match literal spaces, so
# whitespace is flattened first; without that these two pass as clean.
@test "rejects a tab-separated DROP COLUMN" {
  migration "$(printf 'ALTER TABLE tasks\n\tDROP\tCOLUMN foo;')"
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "rejects a tab-separated ALTER COLUMN TYPE" {
  migration "$(printf 'ALTER TABLE t ALTER\tCOLUMN x TYPE text;')"
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'ALTER COLUMN ... TYPE'
}

@test "accepts a purely additive migration" {
  migration 'CREATE TABLE foo (id INT PRIMARY KEY);'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts a tab-indented additive migration" {
  migration "$(printf 'CREATE TABLE foo\n(\n\tid\tINT PRIMARY KEY\n);')"
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts a DROP INDEX, which costs performance rather than correctness" {
  migration 'DROP INDEX IF EXISTS idx_foo;'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts a DROP CONSTRAINT" {
  migration 'ALTER TABLE tasks DROP CONSTRAINT c;'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts a DROP DEFAULT" {
  migration 'ALTER TABLE tasks ALTER COLUMN a DROP DEFAULT;'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts an ADD COLUMN NOT NULL carrying a DEFAULT" {
  migration "ALTER TABLE tasks ADD COLUMN a TEXT NOT NULL DEFAULT '';"
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts an ADD NOT NULL DEFAULT without the optional COLUMN keyword" {
  migration "ALTER TABLE tasks ADD bar TEXT NOT NULL DEFAULT '';"
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts a multi-column ADD where every column has a DEFAULT" {
  migration "ALTER TABLE tasks
  ADD COLUMN IF NOT EXISTS a TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS b INT NOT NULL DEFAULT 0;"
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts an ADD CONSTRAINT, which adds no column" {
  migration 'ALTER TABLE tasks ADD CONSTRAINT c CHECK (a IS NOT NULL);'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "accepts an ADD PRIMARY KEY" {
  migration 'ALTER TABLE tasks ADD PRIMARY KEY (id);'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "ignores a destructive keyword inside a comment" {
  migration '-- we will DROP COLUMN foo one day
CREATE INDEX idx_x ON tasks (app);'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

@test "ignores a destructive keyword inside a block comment" {
  migration '/* one day we will
   DROP COLUMN foo */
CREATE INDEX idx_x ON tasks (app);'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

# A commented-out floor update is not an executable one, so it must not satisfy
# the pairing that lets a destructive migration through.
@test "rejects a DROP COLUMN whose floor update is only inside a block comment" {
  migration 'ALTER TABLE tasks DROP COLUMN foo;
/* UPDATE schema_compatibility SET min_bundled_version = 10; */'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "rejects a DROP COLUMN whose floor update is only inside a line comment" {
  migration 'ALTER TABLE tasks DROP COLUMN foo;
-- UPDATE schema_compatibility SET min_bundled_version = 10;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "rejects a DROP COLUMN whose floor update is only inside a string literal" {
  migration "ALTER TABLE tasks DROP COLUMN foo;
INSERT INTO audit (note) VALUES ('UPDATE schema_compatibility SET min_bundled_version = 10');"
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "ignores a destructive keyword inside a string literal" {
  migration "INSERT INTO audit (note) VALUES ('DROP COLUMN foo was considered');"
  run "$LINT" "$MIGRATIONS"
  assert_success
}

# A doubled quote escapes one inside a literal, so the strip must not treat it as
# a closing quote and spill back into executable SQL.
@test "handles an escaped quote inside a string literal" {
  migration "ALTER TABLE tasks DROP COLUMN foo;
INSERT INTO audit (note) VALUES ('it''s gone');"
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "accepts a DROP COLUMN that records a compatibility floor" {
  migration 'ALTER TABLE tasks DROP COLUMN foo;
UPDATE schema_compatibility SET min_bundled_version = GREATEST(min_bundled_version, 10);'
  run "$LINT" "$MIGRATIONS"
  assert_success
}

# A floor of zero satisfies the pairing while protecting nothing.
@test "rejects a DROP COLUMN whose recorded floor is zero" {
  migration 'ALTER TABLE tasks DROP COLUMN foo;
UPDATE schema_compatibility SET min_bundled_version = 0;'
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP COLUMN'
}

@test "accepts a destructive migration grandfathered by its version" {
  migration 'ALTER TABLE tasks DROP COLUMN foo;' 000002_old.up.sql
  run "$LINT" "$MIGRATIONS"
  assert_success
}

# A name the version arithmetic cannot parse must not abort the loop and leave the
# run reporting success over the migrations it never reached.
@test "rejects a file with no numeric prefix" {
  migration 'CREATE TABLE foo (id INT);' abc_x.up.sql
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'does not follow the NNNNNN_description.up.sql convention'
}

@test "rejects a file with no underscore separator" {
  migration 'CREATE TABLE foo (id INT);' 000031.up.sql
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'does not follow the NNNNNN_description.up.sql convention'
}

@test "rejects a file whose prefix is only partly numeric" {
  migration 'CREATE TABLE foo (id INT);' 12ab_x.up.sql
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'does not follow the NNNNNN_description.up.sql convention'
}

@test "still catches a destructive migration sitting beside a misnamed file" {
  migration 'CREATE TABLE a (id INT);' 000031.up.sql
  migration 'DROP TABLE tasks;' 000032_z.up.sql
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'DROP TABLE'
}

# An unmatched glob must not read as a pass.
@test "rejects an empty migrations directory" {
  run "$LINT" "$MIGRATIONS"
  assert_failure
  assert_output --partial 'no *.up.sql found'
}

@test "accepts the repository's own migrations" {
  run "$LINT" "${BATS_TEST_DIRNAME}/../db/migrations"
  assert_success
  assert_output --partial 'all forward-only'
}
