CREATE TABLE worker_pools (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    concurrency INTEGER NOT NULL DEFAULT 4
        CHECK (concurrency BETWEEN 1 AND 1000),
    status      TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX worker_pools_project_id_idx ON worker_pools (project_id, created_at DESC);
CREATE INDEX worker_pools_active_idx ON worker_pools (status) WHERE status = 'active';

ALTER TABLE worker_tokens
    ADD COLUMN pool_id UUID REFERENCES worker_pools (id) ON DELETE CASCADE,
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'worker'
        CHECK (kind IN ('worker', 'enrollment')),
    ADD COLUMN consumed_at TIMESTAMPTZ;

CREATE INDEX worker_tokens_pool_idx
    ON worker_tokens (pool_id, kind, created_at DESC);

ALTER TABLE workers
    ADD COLUMN worker_pool_id UUID REFERENCES worker_pools (id) ON DELETE SET NULL;

CREATE INDEX workers_pool_id_idx ON workers (worker_pool_id);
