ALTER TABLE worker_pools
    ADD COLUMN queue_names TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN job_timeout_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (job_timeout_seconds BETWEEN 0 AND 86400),
    ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 0
        CHECK (max_attempts BETWEEN 0 AND 100),
    ADD COLUMN retry_backoff TEXT NOT NULL DEFAULT 'exponential'
        CHECK (retry_backoff IN ('fixed', 'exponential')),
    ADD COLUMN retry_base_delay_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (retry_base_delay_seconds BETWEEN 0 AND 86400),
    ADD COLUMN retry_max_delay_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (retry_max_delay_seconds BETWEEN 0 AND 86400);
