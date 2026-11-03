-- Set once ArgoCD answered for the task's application, so the task lookup may use the
-- app name as a metric label without letting an unchecked submission mint a series.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS app_confirmed BOOLEAN NOT NULL DEFAULT false;
