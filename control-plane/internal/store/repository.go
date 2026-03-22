// Package store defines repository interfaces and sentinel errors for the
// Control Plane persistence layer. Implementations live in sub-packages
// (e.g. store/postgres).
package store

import (
	"context"
	"errors"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// Sentinel errors returned by all repository implementations.
var (
	// ErrProjectNotFound is returned when a project slug does not exist in the store.
	ErrProjectNotFound = errors.New("project not found")

	// ErrProjectAlreadyExists is returned when attempting to create a project
	// whose slug already exists (including destroyed projects).
	ErrProjectAlreadyExists = errors.New("project already exists")

	// ErrConfigNotFound is returned when no config rows exist for a given project slug.
	ErrConfigNotFound = errors.New("config not found")

	// ErrStoreUnavailable is returned when the backing store cannot be reached
	// (e.g. DB connection failure, timeout).
	ErrStoreUnavailable = errors.New("store unavailable")

	// ErrStoreInternal is returned for unexpected store-layer errors that do not
	// map to a more specific sentinel (e.g. DB CHECK constraint violation).
	ErrStoreInternal = errors.New("store internal error")
)

// ProjectRepository defines persistence operations for projects.
// Implementations are provided by sub-packages (e.g. store/postgres).
type ProjectRepository interface {
	// Create inserts a new project.
	// Returns ErrProjectAlreadyExists if the slug already exists (including destroyed ones).
	Create(ctx context.Context, project *domain.ProjectModel) error

	// GetBySlug retrieves a project by slug.
	// Returns ErrProjectNotFound if the slug does not exist.
	// Destroyed projects are returned normally (not treated as missing).
	// Health is a runtime-only field and is always nil in the returned model.
	// DB NULL in previous_status maps to the Go zero value ("").
	GetBySlug(ctx context.Context, slug string) (*domain.ProjectModel, error)

	// List returns projects ordered by created_at ASC.
	// By default, destroyed projects are excluded.
	// Pass WithStatus(domain.StatusDestroyed) to include only destroyed projects.
	List(ctx context.Context, filters ...ListFilter) ([]*domain.ProjectModel, error)

	// UpdateStatus updates the project's status, previous_status, and last_error.
	// Returns ErrProjectNotFound if 0 rows are affected.
	// previousStatus of "" is stored as NULL. lastError of "" clears the column (NULLIF).
	// The DB layer does not validate transition legality — that is the domain layer's job.
	UpdateStatus(ctx context.Context, slug string, status, previousStatus domain.ProjectStatus, lastError string) error

	// Delete soft-deletes a project by setting status = 'destroyed'.
	// Returns ErrProjectNotFound if 0 rows are affected.
	// Config rows are retained for audit purposes.
	Delete(ctx context.Context, slug string) error

	// Exists reports whether the slug exists (including destroyed projects).
	// This is a best-effort pre-check; uniqueness is ultimately enforced by Create().
	Exists(ctx context.Context, slug string) (bool, error)
}

// ListFilter is a functional option for ProjectRepository.List.
type ListFilter func(*ListOptions)

// ListOptions holds the resolved filter state for List queries.
type ListOptions struct {
	Status *domain.ProjectStatus
}

// WithStatus filters List results to only projects in the given status.
func WithStatus(status domain.ProjectStatus) ListFilter {
	return func(o *ListOptions) {
		o.Status = &status
	}
}

// ConfigRepository defines persistence operations for project configuration.
type ConfigRepository interface {
	// SaveConfig upserts all resolved config entries for a project.
	// Uses INSERT ... ON CONFLICT DO UPDATE semantics (idempotent).
	// is_secret and category are derived from domain.ConfigSchema().
	SaveConfig(ctx context.Context, projectSlug string, config *domain.ProjectConfig) error

	// GetConfig retrieves the full resolved config for a project.
	// Returns ErrConfigNotFound if no rows exist for the given slug.
	GetConfig(ctx context.Context, projectSlug string) (*domain.ProjectConfig, error)

	// SaveOverrides replaces all overrides for a project atomically
	// (DELETE then INSERT in a single transaction).
	// An empty map performs only the DELETE, clearing all overrides.
	SaveOverrides(ctx context.Context, projectSlug string, overrides map[string]string) error

	// GetOverrides retrieves user-supplied override key→value pairs.
	// Returns an empty (non-nil) map when no overrides exist.
	GetOverrides(ctx context.Context, projectSlug string) (map[string]string, error)

	// DeleteConfig hard-deletes all config and override rows for a project.
	// This is NOT called during normal destroy flows (config is retained for audit).
	// Use only for explicit data removal (e.g. GDPR requests, test teardown).
	DeleteConfig(ctx context.Context, projectSlug string) error
}

// Store combines ProjectRepository and ConfigRepository for convenient injection.
// If Config operations require independent scaling or mocking in future phases,
// split them into separate injection points.
type Store interface {
	ProjectRepository
	ConfigRepository
}
