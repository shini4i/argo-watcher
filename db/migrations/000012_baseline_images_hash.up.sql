-- The hash of the application's images before the deployment changed them. A replica
-- resuming the task compares against it; recomputed then, it would already include the
-- new images and a Degraded rollout could never be told apart from a pre-existing one.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS baseline_images_hash BYTEA;
