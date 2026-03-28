// Package postgres provides PostgreSQL-backed implementations of the store interfaces.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// pgErrUniqueViolation is the PostgreSQL error code for unique constraint violations.
const pgErrUniqueViolation = "23505"

// DB is the interface satisfied by *pgxpool.Pool and *pgx.Conn, allowing
// either to be injected without changing repository code.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ProjectRepository implements store.ProjectRepository against a
// PostgreSQL database (via pgx).
type ProjectRepository struct {
	db DB
}

// NewProjectRepository constructs a repository backed by the given pool.
func NewProjectRepository(pool *pgxpool.Pool) *ProjectRepository {
	return &ProjectRepository{db: pool}
}

// Create inserts a new project row.
// If a destroyed row already exists with the same slug, it and its associated
// config rows are replaced within a transaction so slugs can be reused after deletion.
// Returns store.ErrProjectAlreadyExists if an active (non-destroyed) row exists.
func (r *ProjectRepository) Create(ctx context.Context, project *domain.ProjectModel) error {
	const insertQ = `
		INSERT INTO projects (slug, display_name, runtime_type, status, previous_status, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)`

	_, err := r.db.Exec(ctx, insertQ,
		project.Slug,
		project.DisplayName,
		string(project.RuntimeType),
		string(project.Status),
		string(project.PreviousStatus),
		project.LastError,
		project.CreatedAt,
		project.UpdatedAt,
	)
	if err == nil {
		return nil
	}

	mappedErr := mapPgError(err)
	if !errors.Is(mappedErr, store.ErrProjectAlreadyExists) {
		return mappedErr
	}

	// Unique violation: check whether the existing row is destroyed.
	// If so, purge it (and its config) and retry the insert within a transaction.
	const statusQ = `SELECT status FROM projects WHERE slug = $1`
	var existingStatus string
	if scanErr := r.db.QueryRow(ctx, statusQ, project.Slug).Scan(&existingStatus); scanErr != nil {
		return mapPgError(scanErr)
	}
	if existingStatus != string(domain.StatusDestroyed) {
		return store.ErrProjectAlreadyExists
	}

	// Replace the destroyed row inside a transaction.
	tx, txErr := r.db.Begin(ctx)
	if txErr != nil {
		return mapPgError(txErr)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, txErr = tx.Exec(ctx, `DELETE FROM project_overrides WHERE project_slug = $1`, project.Slug); txErr != nil {
		return mapPgError(txErr)
	}
	if _, txErr = tx.Exec(ctx, `DELETE FROM project_configs WHERE project_slug = $1`, project.Slug); txErr != nil {
		return mapPgError(txErr)
	}
	if _, txErr = tx.Exec(ctx, `DELETE FROM projects WHERE slug = $1`, project.Slug); txErr != nil {
		return mapPgError(txErr)
	}
	if _, txErr = tx.Exec(ctx, insertQ,
		project.Slug,
		project.DisplayName,
		string(project.RuntimeType),
		string(project.Status),
		string(project.PreviousStatus),
		project.LastError,
		project.CreatedAt,
		project.UpdatedAt,
	); txErr != nil {
		return mapPgError(txErr)
	}
	return mapPgError(tx.Commit(ctx))
}

// GetBySlug retrieves a project by its slug.
// Returns store.ErrProjectNotFound when no row matches.
func (r *ProjectRepository) GetBySlug(ctx context.Context, slug string) (*domain.ProjectModel, error) {
	const q = `
		SELECT slug, display_name, runtime_type, status,
		       COALESCE(previous_status, '') AS previous_status,
		       COALESCE(last_error, '')      AS last_error,
		       created_at, updated_at
		FROM   projects
		WHERE  slug = $1`

	row := r.db.QueryRow(ctx, q, slug)
	p := &domain.ProjectModel{}
	var status, previousStatus, runtimeType string
	err := row.Scan(
		&p.Slug,
		&p.DisplayName,
		&runtimeType,
		&status,
		&previousStatus,
		&p.LastError,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrProjectNotFound
		}
		return nil, fmt.Errorf("%w: GetBySlug: %w", store.ErrStoreInternal, err)
	}
	p.RuntimeType = domain.RuntimeType(runtimeType)
	if err := domain.ValidateRuntimeType(p.RuntimeType); err != nil {
		return nil, fmt.Errorf("%w: GetBySlug: invalid runtime_type %q in database", store.ErrStoreInternal, runtimeType)
	}
	p.Status = domain.ProjectStatus(status)
	p.PreviousStatus = domain.ProjectStatus(previousStatus)
	// Health is runtime-only and is not persisted; callers must populate it
	// from the runtime adapter after loading a project.
	p.Health = nil
	return p, nil
}

// List returns projects ordered by created_at ASC.
// Without filters, destroyed projects are excluded.
// Use WithStatus(domain.StatusDestroyed) to query only destroyed projects.
func (r *ProjectRepository) List(ctx context.Context, filters ...store.ListFilter) ([]*domain.ProjectModel, error) {
	opts := &store.ListOptions{}
	for _, f := range filters {
		f(opts)
	}

	var q string
	var args []any
	if opts.Status != nil {
		q = `
			SELECT slug, display_name, runtime_type, status,
			       COALESCE(previous_status, '') AS previous_status,
			       COALESCE(last_error, '')      AS last_error,
			       created_at, updated_at
			FROM   projects
			WHERE  status = $1
			ORDER  BY created_at ASC`
		args = []any{string(*opts.Status)}
	} else {
		q = `
			SELECT slug, display_name, runtime_type, status,
			       COALESCE(previous_status, '') AS previous_status,
			       COALESCE(last_error, '')      AS last_error,
			       created_at, updated_at
			FROM   projects
			WHERE  status != 'destroyed'
			ORDER  BY created_at ASC`
	}

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	var projects []*domain.ProjectModel
	for rows.Next() {
		p := &domain.ProjectModel{}
		var status, previousStatus, runtimeType string
		if err := rows.Scan(
			&p.Slug, &p.DisplayName, &runtimeType, &status, &previousStatus,
			&p.LastError, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("%w: List scan: %w", store.ErrStoreInternal, err)
		}
		p.RuntimeType = domain.RuntimeType(runtimeType)
		if err := domain.ValidateRuntimeType(p.RuntimeType); err != nil {
			return nil, fmt.Errorf("%w: List: invalid runtime_type %q in database for slug %q", store.ErrStoreInternal, runtimeType, p.Slug)
		}
		p.Status = domain.ProjectStatus(status)
		p.PreviousStatus = domain.ProjectStatus(previousStatus)
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return projects, nil
}

// UpdateStatus updates a project's status, previous_status, and last_error atomically.
// Returns store.ErrProjectNotFound when no row is affected.
// previousStatus of "" is stored as NULL. lastError of "" clears the column.
func (r *ProjectRepository) UpdateStatus(
	ctx context.Context,
	slug string,
	status, previousStatus domain.ProjectStatus,
	lastError string,
) error {
	const q = `
		UPDATE projects
		SET    status          = $2,
		       previous_status = NULLIF($3, ''),
		       last_error      = NULLIF($4, ''),
		       updated_at      = now()
		WHERE  slug = $1`

	tag, err := r.db.Exec(ctx, q, slug, string(status), string(previousStatus), lastError)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrProjectNotFound
	}
	return nil
}

// Delete soft-deletes a project by setting its status to 'destroyed'.
// Returns store.ErrProjectNotFound when no row is affected.
func (r *ProjectRepository) Delete(ctx context.Context, slug string) error {
	const q = `UPDATE projects SET status = 'destroyed', updated_at = now() WHERE slug = $1`
	tag, err := r.db.Exec(ctx, q, slug)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrProjectNotFound
	}
	return nil
}

// Exists reports whether any project (including destroyed ones) has the given slug.
func (r *ProjectRepository) Exists(ctx context.Context, slug string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM projects WHERE slug = $1)`
	var exists bool
	err := r.db.QueryRow(ctx, q, slug).Scan(&exists)
	if err != nil {
		return false, mapPgError(err)
	}
	return exists, nil
}

// mapPgError converts pgx errors to store sentinel errors.
func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgErrUniqueViolation:
			return store.ErrProjectAlreadyExists
		}
		return fmt.Errorf("%w: pgsql %s: %s", store.ErrStoreInternal, pgErr.Code, pgErr.Message)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %w", store.ErrStoreUnavailable, err)
}
