ALTER TABLE api_keys
    ADD COLUMN key_prefix TEXT NOT NULL DEFAULT '',
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD COLUMN last_used_at TIMESTAMPTZ;

CREATE INDEX api_keys_active_expiry_idx
    ON api_keys (expires_at)
    WHERE revoked_at IS NULL AND expires_at IS NOT NULL;
