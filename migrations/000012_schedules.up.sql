-- Recurring schedules: cron-driven job templates evaluated by the API.

CREATE TABLE schedules (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    cron         TEXT NOT NULL,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    enabled      BOOLEAN NOT NULL DEFAULT true,
    job_type     TEXT NOT NULL,
    queue_name   TEXT NOT NULL DEFAULT '',
    payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    priority     INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 0,
    next_run_at  TIMESTAMPTZ NOT NULL,
    last_run_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX schedules_due_idx
    ON schedules (next_run_at)
    WHERE enabled;

CREATE INDEX schedules_project_idx
    ON schedules (project_id, created_at DESC);
