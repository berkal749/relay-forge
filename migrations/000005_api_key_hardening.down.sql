DROP INDEX IF EXISTS api_keys_active_expiry_idx;

ALTER TABLE api_keys
    DROP COLUMN key_prefix,
    DROP COLUMN expires_at,
    DROP COLUMN last_used_at;
