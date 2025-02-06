CREATE INDEX IF NOT EXISTS idx_tasks_created_app ON tasks (created DESC, app);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_id ON tasks (id);

-- Drop indexes that are no longer needed
DROP INDEX IF EXISTS tasks_idx_created;
DROP INDEX IF EXISTS idx_tasks_status;
