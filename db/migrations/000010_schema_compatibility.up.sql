-- The oldest build allowed to run against this schema, as a migration version. A
-- build whose newest bundled migration is below it refuses to start, because a
-- migration recorded here removed something that build still needs.
CREATE TABLE IF NOT EXISTS schema_compatibility
(
    id                  INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- Zero means every build that knows this table can run. Only a destructive
    -- migration raises it, in its own file, to the version at which the code
    -- stopped using whatever that migration removes.
    min_bundled_version INT NOT NULL DEFAULT 0
);

INSERT INTO schema_compatibility (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
