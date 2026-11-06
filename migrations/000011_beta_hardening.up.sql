-- Beta hardening: admin roles, invite-only registration, and quota bookkeeping.

ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'user'
        CHECK (role IN ('user', 'admin'));

CREATE TABLE invites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash  TEXT NOT NULL UNIQUE,
    email       TEXT,
    role        TEXT NOT NULL DEFAULT 'user'
        CHECK (role IN ('user', 'admin')),
    created_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    consumed_by UUID REFERENCES users (id) ON DELETE SET NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX invites_created_at_idx ON invites (created_at DESC);
CREATE INDEX invites_pending_idx ON invites (expires_at) WHERE consumed_at IS NULL;

CREATE INDEX worker_pools_project_status_idx
    ON worker_pools (project_id, status);

CREATE INDEX jobs_project_status_idx
    ON jobs (project_id, status);
