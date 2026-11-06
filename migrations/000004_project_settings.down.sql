ALTER TABLE projects
    DROP COLUMN default_queue_name,
    DROP COLUMN default_max_attempts,
    DROP COLUMN default_priority,
    DROP COLUMN job_retention_days,
    DROP COLUMN max_concurrency,
    DROP COLUMN rate_limit_per_minute,
    DROP COLUMN default_job_timeout_seconds;
