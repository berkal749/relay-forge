-- Encrypted, project-scoped notification destinations.

CREATE TABLE notification_channels (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL
        CHECK (kind IN ('slack', 'discord', 'telegram', 'sms', 'email')),
    config     BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX notification_channels_project_idx
    ON notification_channels (project_id, created_at DESC);
