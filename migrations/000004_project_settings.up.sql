ALTER TABLE projects
    ADD COLUMN default_queue_name TEXT NOT NULL DEFAULT 'default'
        CHECK (char_length(default_queue_name) BETWEEN 1 AND 100),
    ADD COLUMN default_max_attempts INTEGER NOT NULL DEFAULT 3
        CHECK (default_max_attempts BETWEEN 1 AND 100),
    ADD COLUMN default_priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN job_retention_days INTEGER NOT NULL DEFAULT 30
        CHECK (job_retention_days BETWEEN 0 AND 3650),
    ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 0
        CHECK (max_concurrency BETWEEN 0 AND 1000),
    ADD COLUMN rate_limit_per_minute INTEGER NOT NULL DEFAULT 0
        CHECK (rate_limit_per_minute BETWEEN 0 AND 1000000),
    ADD COLUMN default_job_timeout_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (default_job_timeout_seconds BETWEEN 0 AND 86400);
