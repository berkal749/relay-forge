DROP INDEX IF EXISTS jobs_project_idempotency_key_idx;

ALTER TABLE jobs
    DROP COLUMN idempotency_key,
    DROP COLUMN idempotency_hash;
