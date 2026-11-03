-- Application name prefixes a token covers, alongside the exact names in apps. A
-- build predating this column reads a prefix-only token as covering nothing, so a
-- mixed-version rollout fails closed rather than widening any token.
ALTER TABLE app_tokens ADD COLUMN IF NOT EXISTS app_prefixes JSONB NOT NULL DEFAULT '[]'::jsonb;

-- jsonb_typeof guards both arrays for the reason 000009 gives: JSON null would make
-- jsonb_array_length return SQL NULL, which a CHECK passes.
ALTER TABLE app_tokens DROP CONSTRAINT IF EXISTS app_tokens_scope_exclusive;
ALTER TABLE app_tokens ADD CONSTRAINT app_tokens_scope_exclusive CHECK (
    jsonb_typeof(apps) = 'array'
        AND jsonb_typeof(app_prefixes) = 'array'
        AND ((all_apps AND jsonb_array_length(apps) = 0 AND jsonb_array_length(app_prefixes) = 0)
            OR (NOT all_apps AND jsonb_array_length(apps) + jsonb_array_length(app_prefixes) > 0))
    );
