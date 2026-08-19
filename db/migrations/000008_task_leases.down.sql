DROP INDEX IF EXISTS idx_tasks_claimable;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS owner_id,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS timeout,
    DROP COLUMN IF EXISTS refresh;
