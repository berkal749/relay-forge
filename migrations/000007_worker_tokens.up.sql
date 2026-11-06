CREATE TABLE worker_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    name         TEXT NOT NULL,
    revoked_at   TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX worker_tokens_project_id_idx ON worker_tokens (project_id, created_at DESC);

CREATE INDEX worker_tokens_active_expiry_idx
    ON worker_tokens (expires_at)
    WHERE revoked_at IS NULL AND expires_at IS NOT NULL;

ALTER TABLE workers
    ADD COLUMN worker_token_id UUID REFERENCES worker_tokens (id) ON DELETE SET NULL;

CREATE INDEX workers_token_id_idx ON workers (worker_token_id);
