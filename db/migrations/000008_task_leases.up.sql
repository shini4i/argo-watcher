-- Task ownership for HA. A task is monitored by exactly one replica at a time:
-- owner_id names it and lease_expires_at is the instant its claim lapses. A row
-- whose lease has lapsed (or was never taken) is available for another replica
-- to claim and resume, so a deployment survives losing the pod that accepted it.
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS owner_id         TEXT,
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    -- The per-task rollout deadline and refresh override arrive on the request and
    -- until now lived only in the accepting process. A resumed task must keep the
    -- settings it was accepted with rather than inheriting the new owner's defaults.
    ADD COLUMN IF NOT EXISTS timeout          INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS refresh          BOOLEAN;

-- Only in-progress rows are ever claimed, so the partial index keeps each claim
-- scan proportional to the number of live deployments rather than to the whole
-- task history. NULL leases are indexed too: an unowned row is claimable.
--
-- Built without CONCURRENTLY, which blocks writes to tasks until it completes.
-- The migrator runs each file in a transaction, where CONCURRENTLY is not
-- allowed, and the build scans a table holding one row per deployment ever made:
-- brief at any realistic history size, and it runs in the pre-upgrade hook while
-- the previous version is still serving.
CREATE INDEX IF NOT EXISTS idx_tasks_claimable
    ON tasks (lease_expires_at) WHERE status = 'in progress';
