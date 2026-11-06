DROP INDEX IF EXISTS workers_token_id_idx;

ALTER TABLE workers
    DROP COLUMN worker_token_id;

DROP INDEX IF EXISTS worker_tokens_active_expiry_idx;
DROP INDEX IF EXISTS worker_tokens_project_id_idx;

DROP TABLE worker_tokens;
