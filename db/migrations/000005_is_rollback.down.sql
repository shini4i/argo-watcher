ALTER TABLE tasks DROP COLUMN IF EXISTS rollback_target_id;
ALTER TABLE tasks DROP COLUMN IF EXISTS is_rollback;
