DROP INDEX IF EXISTS workers_pool_id_idx;

ALTER TABLE workers
    DROP COLUMN worker_pool_id;

DROP INDEX IF EXISTS worker_tokens_pool_idx;

ALTER TABLE worker_tokens
    DROP COLUMN consumed_at,
    DROP COLUMN kind,
    DROP COLUMN pool_id;

DROP INDEX IF EXISTS worker_pools_active_idx;
DROP INDEX IF EXISTS worker_pools_project_id_idx;

DROP TABLE worker_pools;
