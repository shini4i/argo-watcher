-- idx_tasks_id duplicated the primary key, adding only write and storage cost.
DROP INDEX IF EXISTS idx_tasks_id;

-- Lets the hourly app-not-found sweep seek instead of reading the whole table. The
-- predicate must match models.StatusAppNotFoundMessage. Built without CONCURRENTLY
-- (see 000008), so it blocks writes to tasks for one scan.
CREATE INDEX IF NOT EXISTS idx_tasks_app_not_found
    ON tasks (created) WHERE status = 'app not found';
