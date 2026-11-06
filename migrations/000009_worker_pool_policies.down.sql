ALTER TABLE worker_pools
    DROP COLUMN retry_max_delay_seconds,
    DROP COLUMN retry_base_delay_seconds,
    DROP COLUMN retry_backoff,
    DROP COLUMN max_attempts,
    DROP COLUMN job_timeout_seconds,
    DROP COLUMN queue_names;
