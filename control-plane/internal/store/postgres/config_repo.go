package postgres

import (
	"context"
	"fmt"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// categoryString maps domain.ConfigCategory to the DB CHECK constraint strings.
var categoryString = map[domain.ConfigCategory]string{
	domain.CategoryStaticDefault:    "static_default",
	domain.CategoryPerProject:       "per_project",
	domain.CategoryGeneratedSecret:  "generated_secret",
	domain.CategoryUserOverridable:  "user_overridable",
}

// ConfigRepository implements store.ConfigRepository against PostgreSQL.
type ConfigRepository struct {
	db DB
}

// NewConfigRepository constructs a repository backed by the given pool.
func NewConfigRepository(db DB) *ConfigRepository {
	return &ConfigRepository{db: db}
}

// SaveConfig upserts all resolved config entries for a project.
// Category and is_secret metadata are derived from domain.ConfigSchema().
// Idempotent: safe to call multiple times.
func (r *ConfigRepository) SaveConfig(ctx context.Context, projectSlug string, config *domain.ProjectConfig) error {
	if len(config.Values) == 0 {
		return nil
	}

	// Build a lookup for metadata from the schema.
	schema := domain.ConfigSchema()
	meta := make(map[string]domain.ConfigEntry, len(schema))
	for _, e := range schema {
		meta[e.Key] = e
	}

	const q = `
		INSERT INTO project_configs (project_slug, key, value, is_secret, category)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (project_slug, key) DO UPDATE
		  SET value      = EXCLUDED.value,
		      is_secret  = EXCLUDED.is_secret,
		      category   = EXCLUDED.category,
		      updated_at = now()`

	for key, value := range config.Values {
		entry, ok := meta[key]
		if !ok {
			// Unknown keys (not in schema) are skipped — the schema is authoritative.
			continue
		}
		catStr, ok := categoryString[entry.Category]
		if !ok {
			return fmt.Errorf("%w: unknown category for key %q", store.ErrStoreInternal, key)
		}
		isSecret := entry.Category == domain.CategoryGeneratedSecret

		if _, err := r.db.Exec(ctx, q, projectSlug, key, value, isSecret, catStr); err != nil {
			return mapPgError(err)
		}
	}
	return nil
}

// GetConfig retrieves the full resolved config for a project.
// Returns store.ErrConfigNotFound if no rows exist for the slug.
func (r *ConfigRepository) GetConfig(ctx context.Context, projectSlug string) (*domain.ProjectConfig, error) {
	const q = `
		SELECT key, value
		FROM   project_configs
		WHERE  project_slug = $1`

	rows, err := r.db.Query(ctx, q, projectSlug)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("%w: GetConfig scan: %w", store.ErrStoreInternal, err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	if len(values) == 0 {
		return nil, store.ErrConfigNotFound
	}

	return &domain.ProjectConfig{
		ProjectSlug: projectSlug,
		Values:      values,
		Overrides:   map[string]string{},
	}, nil
}

// SaveOverrides atomically replaces all overrides for a project.
// DELETE is always executed; INSERT is skipped when overrides is empty.
func (r *ConfigRepository) SaveOverrides(ctx context.Context, projectSlug string, overrides map[string]string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin transaction: %w", store.ErrStoreUnavailable, err)
	}
	defer func() {
		// Best-effort rollback; error is swallowed since we only care about
		// the original error returned to the caller.
		_ = tx.Rollback(ctx)
	}()

	const del = `DELETE FROM project_overrides WHERE project_slug = $1`
	if _, err := tx.Exec(ctx, del, projectSlug); err != nil {
		return mapPgError(err)
	}

	if len(overrides) > 0 {
		const ins = `
			INSERT INTO project_overrides (project_slug, key, value)
			VALUES ($1, $2, $3)`
		for key, value := range overrides {
			if _, err := tx.Exec(ctx, ins, projectSlug, key, value); err != nil {
				return mapPgError(err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit transaction: %w", store.ErrStoreUnavailable, err)
	}
	return nil
}

// GetOverrides retrieves user-supplied override key→value pairs.
// Returns an empty (non-nil) map when no overrides exist.
func (r *ConfigRepository) GetOverrides(ctx context.Context, projectSlug string) (map[string]string, error) {
	const q = `SELECT key, value FROM project_overrides WHERE project_slug = $1`
	rows, err := r.db.Query(ctx, q, projectSlug)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	overrides := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("%w: GetOverrides scan: %w", store.ErrStoreInternal, err)
		}
		overrides[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return overrides, nil
}

// DeleteConfig hard-deletes all config and override rows for the project.
// This is NOT part of the normal destroy flow; use only for explicit data removal.
func (r *ConfigRepository) DeleteConfig(ctx context.Context, projectSlug string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin transaction: %w", store.ErrStoreUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM project_overrides WHERE project_slug = $1`, projectSlug); err != nil {
		return mapPgError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_configs WHERE project_slug = $1`, projectSlug); err != nil {
		return mapPgError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit transaction: %w", store.ErrStoreUnavailable, err)
	}
	return nil
}
