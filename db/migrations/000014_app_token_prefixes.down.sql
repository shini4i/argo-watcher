-- A prefix-scoped token has no representation in the old schema. Dropping the column
-- would leave a prefix-only row violating the old constraint, or silently narrow a
-- mixed one, so the rollback refuses until an operator deletes those rows.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM app_tokens WHERE jsonb_array_length(app_prefixes) > 0) THEN
        RAISE EXCEPTION 'app_tokens holds prefix-scoped tokens; delete them before rolling back 000014';
    END IF;
END
$$;

ALTER TABLE app_tokens DROP CONSTRAINT IF EXISTS app_tokens_scope_exclusive;
ALTER TABLE app_tokens DROP COLUMN IF EXISTS app_prefixes;
ALTER TABLE app_tokens ADD CONSTRAINT app_tokens_scope_exclusive CHECK (
    jsonb_typeof(apps) = 'array'
        AND ((all_apps AND jsonb_array_length(apps) = 0)
            OR (NOT all_apps AND jsonb_array_length(apps) > 0))
    );
