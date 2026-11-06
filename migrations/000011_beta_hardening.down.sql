DROP INDEX IF EXISTS jobs_project_status_idx;
DROP INDEX IF EXISTS worker_pools_project_status_idx;
DROP TABLE IF EXISTS invites;
ALTER TABLE users DROP COLUMN IF EXISTS role;
