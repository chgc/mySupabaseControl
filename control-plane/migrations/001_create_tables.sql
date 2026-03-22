-- Migration: 001_create_tables.sql
-- Creates the core tables for the Supabase Control Plane.
-- Run via: supabase db push (or psql -f migrations/001_create_tables.sql)

-- ============================================================
-- shared updated_at trigger function
-- ============================================================
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================================
-- projects
-- ============================================================
CREATE TABLE IF NOT EXISTS projects (
    slug            TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'creating',
    previous_status TEXT,  -- NULL maps to Go ProjectModel.PreviousStatus zero value ("")
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT valid_slug CHECK (
        slug ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$'
        AND length(slug) BETWEEN 3 AND 40
    ),
    -- NOTE: 'destroying' is an intermediate state used during async Destroy operations.
    CONSTRAINT valid_status CHECK (
        status IN (
            'creating', 'stopped', 'starting', 'running',
            'stopping', 'destroying', 'destroyed', 'error'
        )
    ),
    CONSTRAINT valid_previous_status CHECK (
        previous_status IS NULL OR
        previous_status IN (
            'creating', 'stopped', 'starting', 'running',
            'stopping', 'destroying', 'destroyed', 'error'
        )
    ),
    CONSTRAINT valid_display_name CHECK (
        length(display_name) BETWEEN 1 AND 100
    )
);

CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

-- Partial index: only active projects (status != 'destroyed') need fast status lookups.
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects (status)
    WHERE status != 'destroyed';

-- ============================================================
-- project_configs
-- ============================================================
CREATE TABLE IF NOT EXISTS project_configs (
    project_slug TEXT        NOT NULL REFERENCES projects(slug),
    key          TEXT        NOT NULL,
    value        TEXT        NOT NULL,
    is_secret    BOOLEAN     NOT NULL DEFAULT false,
    category     TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (project_slug, key),

    CONSTRAINT valid_category CHECK (
        category IN (
            'static_default', 'per_project',
            'generated_secret', 'user_overridable'
        )
    )
);

CREATE TRIGGER set_config_updated_at
    BEFORE UPDATE ON project_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

-- ============================================================
-- project_overrides
-- ============================================================
-- Stores user-supplied overrides separately from computed config values.
-- A project_overrides row exists only when the user has explicitly set
-- a UserOverridable key to a non-default value.
CREATE TABLE IF NOT EXISTS project_overrides (
    project_slug TEXT        NOT NULL REFERENCES projects(slug),
    key          TEXT        NOT NULL,
    value        TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (project_slug, key)
);

CREATE TRIGGER set_override_updated_at
    BEFORE UPDATE ON project_overrides
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

-- ============================================================
-- Row Level Security
-- ============================================================
-- Phase 1: RLS is enabled but uses USING (true) because the Control Plane
-- backend always connects with the service_role key (bypasses RLS).
-- If PostgREST direct access is ever required, replace with:
--   USING (auth.role() = 'service_role')
ALTER TABLE projects         ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_configs  ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_overrides ENABLE ROW LEVEL SECURITY;

CREATE POLICY "service_role_full_access" ON projects
    FOR ALL USING (true) WITH CHECK (true);

CREATE POLICY "service_role_full_access" ON project_configs
    FOR ALL USING (true) WITH CHECK (true);

CREATE POLICY "service_role_full_access" ON project_overrides
    FOR ALL USING (true) WITH CHECK (true);
