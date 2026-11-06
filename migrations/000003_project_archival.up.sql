ALTER TABLE projects
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN archived_at TIMESTAMPTZ;

CREATE INDEX projects_user_active_idx
    ON projects (user_id, created_at DESC)
    WHERE archived_at IS NULL;

CREATE FUNCTION set_projects_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER projects_set_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW
    EXECUTE FUNCTION set_projects_updated_at();
