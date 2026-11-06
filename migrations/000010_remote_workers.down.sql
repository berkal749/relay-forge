ALTER TABLE worker_pools
    DROP COLUMN allowed_cidrs;

DROP INDEX IF EXISTS worker_tokens_replaced_by_idx;

ALTER TABLE worker_tokens
    DROP COLUMN replaced_by_id,
    DROP COLUMN rotated_at;

ALTER TABLE workers
    DROP COLUMN mode;

DROP INDEX IF EXISTS executions_running_lease_idx;

ALTER TABLE executions
    DROP COLUMN lease_expires_at;
