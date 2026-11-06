DROP TRIGGER IF EXISTS projects_set_updated_at ON projects;
DROP FUNCTION IF EXISTS set_projects_updated_at();
DROP INDEX IF EXISTS projects_user_active_idx;

ALTER TABLE projects
    DROP COLUMN IF EXISTS archived_at,
    DROP COLUMN IF EXISTS updated_at;
