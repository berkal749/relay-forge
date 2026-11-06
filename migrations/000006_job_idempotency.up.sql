ALTER TABLE jobs
    ADD COLUMN idempotency_key TEXT,
    ADD COLUMN idempotency_hash TEXT;

CREATE UNIQUE INDEX jobs_project_idempotency_key_idx
    ON jobs (project_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
