-- Restore the old index on `created`
CREATE INDEX IF NOT EXISTS tasks_idx_created ON tasks (created);

-- Restore the index on `status`
CREATE INDEX IF NOT EXISTS "idx_tasks_status" ON "tasks" ("status");

-- Drop the newly created indexes
DROP INDEX IF EXISTS idx_tasks_created_app;
DROP INDEX IF EXISTS idx_tasks_id;
