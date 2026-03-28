-- Migration: 002_add_runtime_type.sql
-- Adds runtime_type column to support multiple runtime backends.
-- Idempotent: safe to run multiple times.

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS runtime_type TEXT NOT NULL DEFAULT 'docker-compose';

DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'valid_runtime_type'
  ) THEN
    ALTER TABLE projects ADD CONSTRAINT valid_runtime_type CHECK (
        runtime_type IN ('docker-compose', 'kubernetes')
    );
  END IF;
END $$;
