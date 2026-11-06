-- RelayForge initial schema
-- Requires PostgreSQL 13+ for built-in gen_random_uuid().

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX projects_user_id_idx ON projects (user_id);

CREATE TABLE api_keys (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    key_hash   TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX api_keys_project_id_idx ON api_keys (project_id);

CREATE TABLE workers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'offline'
                      CHECK (status IN ('online', 'offline')),
    last_heartbeat_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX workers_project_status_idx ON workers (project_id, status);

CREATE TABLE jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    queue_name   TEXT NOT NULL,
    job_type     TEXT NOT NULL,
    payload      JSONB NOT NULL DEFAULT '{}'::JSONB,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'scheduled', 'running', 'succeeded', 'failed', 'cancelled')),
    priority     INTEGER NOT NULL DEFAULT 0,
    attempts     INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX jobs_project_created_at_idx ON jobs (project_id, created_at DESC);

CREATE INDEX jobs_dispatch_idx
    ON jobs (project_id, queue_name, priority DESC, scheduled_at)
    WHERE status IN ('pending', 'scheduled');

CREATE FUNCTION set_jobs_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER jobs_set_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION set_jobs_updated_at();

CREATE TABLE executions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id         UUID NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    worker_id      UUID REFERENCES workers (id) ON DELETE SET NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    status         TEXT NOT NULL
                   CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    error_message  TEXT,
    result         JSONB,
    UNIQUE (job_id, attempt_number),
    CHECK (finished_at IS NULL OR finished_at >= started_at)
);

CREATE INDEX executions_worker_id_idx ON executions (worker_id);

CREATE TABLE outbox_messages (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id       UUID NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'processing', 'processed', 'failed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX outbox_messages_job_id_idx ON outbox_messages (job_id);

CREATE INDEX outbox_messages_pending_idx
    ON outbox_messages (created_at)
    WHERE status IN ('pending', 'processing');
