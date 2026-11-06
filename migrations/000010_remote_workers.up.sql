-- Remote worker leases, token rotation, pool allowlists, and worker mode.

ALTER TABLE executions
    ADD COLUMN lease_expires_at TIMESTAMPTZ;

CREATE INDEX executions_running_lease_idx
    ON executions (lease_expires_at)
    WHERE status = 'running';

ALTER TABLE workers
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'direct'
        CHECK (mode IN ('direct', 'remote'));

ALTER TABLE worker_tokens
    ADD COLUMN rotated_at TIMESTAMPTZ,
    ADD COLUMN replaced_by_id UUID REFERENCES worker_tokens (id) ON DELETE SET NULL;

CREATE INDEX worker_tokens_replaced_by_idx
    ON worker_tokens (replaced_by_id)
    WHERE replaced_by_id IS NOT NULL;

ALTER TABLE worker_pools
    ADD COLUMN allowed_cidrs TEXT[] NOT NULL DEFAULT '{}';
