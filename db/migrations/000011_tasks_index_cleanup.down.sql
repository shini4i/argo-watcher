DROP INDEX IF EXISTS idx_tasks_app_not_found;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_id ON tasks (id);
