//go:build integration

// Integration tests for the PostgreSQL store implementations.
// These tests require a live PostgreSQL database (e.g. local Supabase instance).
//
// Run with:
//
//	DATABASE_URL="postgresql://postgres:postgres@localhost:54322/postgres" \
//	  go test -v -race -tags integration ./internal/store/postgres/...
package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
	"github.com/kevin/supabase-control-plane/internal/store/postgres"
)

// testDB opens a pgxpool from DATABASE_URL env var. Skips if not set.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })
	return pool
}

// uniqueSlug creates a test-scoped slug that won't collide between runs.
func uniqueSlug(t *testing.T) string {
	t.Helper()
	// Use first 8 chars of test name, sanitised + a suffix.
	return "it-test"
}

// cleanupSlug deletes test project and its config rows after a test.
func cleanupSlug(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM project_overrides WHERE project_slug = $1`, slug)
	_, _ = pool.Exec(ctx, `DELETE FROM project_configs WHERE project_slug = $1`, slug)
	_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE slug = $1`, slug)
}

// allSecrets returns all 12 GeneratedSecret values for test use.
func allSecrets() map[string]string {
	return map[string]string{
		"JWT_SECRET":                    "test-jwt-secret",
		"ANON_KEY":                      "test-anon",
		"SERVICE_ROLE_KEY":              "test-service",
		"POSTGRES_PASSWORD":             "test-pg-pw",
		"DASHBOARD_PASSWORD":            "test-dash",
		"SECRET_KEY_BASE":               "test-secret-key-base",
		"VAULT_ENC_KEY":                 "test-vault-enc",
		"PG_META_CRYPTO_KEY":            "test-pg-meta-crypto",
		"LOGFLARE_PUBLIC_ACCESS_TOKEN":  "test-logflare-pub",
		"LOGFLARE_PRIVATE_ACCESS_TOKEN": "test-logflare-priv",
		"S3_PROTOCOL_ACCESS_KEY_ID":     "test-s3-key-id",
		"S3_PROTOCOL_ACCESS_KEY_SECRET": "test-s3-key-secret",
	}
}

// ─── ProjectRepository round-trips ───────────────────────────────────────────

func TestIntegration_ProjectRepository_CreateAndGetBySlug(t *testing.T) {
	pool := testDB(t)
	slug := "int-create-get"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	project, err := domain.NewProject(slug, "Integration Test Project")
	require.NoError(t, err)

	require.NoError(t, repo.Create(ctx, project))

	got, err := repo.GetBySlug(ctx, slug)
	require.NoError(t, err)
	assert.Equal(t, slug, got.Slug)
	assert.Equal(t, "Integration Test Project", got.DisplayName)
	assert.Equal(t, domain.StatusCreating, got.Status)
	assert.Equal(t, domain.ProjectStatus(""), got.PreviousStatus)
	assert.Nil(t, got.Health)
}

func TestIntegration_ProjectRepository_Create_DuplicateSlug(t *testing.T) {
	pool := testDB(t)
	slug := "int-dup-slug"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	p, _ := domain.NewProject(slug, "First")
	require.NoError(t, repo.Create(ctx, p))

	p2, _ := domain.NewProject(slug, "Second")
	err := repo.Create(ctx, p2)
	assert.ErrorIs(t, err, store.ErrProjectAlreadyExists)
}

func TestIntegration_ProjectRepository_GetBySlug_NotFound(t *testing.T) {
	pool := testDB(t)
	repo := postgres.NewProjectRepository(pool)
	_, err := repo.GetBySlug(context.Background(), "nonexistent-slug-xyz")
	assert.ErrorIs(t, err, store.ErrProjectNotFound)
}

func TestIntegration_ProjectRepository_List(t *testing.T) {
	pool := testDB(t)
	slug := "int-list-test"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	p, _ := domain.NewProject(slug, "List Test")
	require.NoError(t, repo.Create(ctx, p))

	projects, err := repo.List(ctx)
	require.NoError(t, err)

	var found bool
	for _, proj := range projects {
		if proj.Slug == slug {
			found = true
		}
		// Default list must exclude destroyed projects.
		assert.NotEqual(t, domain.StatusDestroyed, proj.Status)
	}
	assert.True(t, found, "created project must appear in List()")
}

func TestIntegration_ProjectRepository_UpdateStatus(t *testing.T) {
	pool := testDB(t)
	slug := "int-update-status"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	p, _ := domain.NewProject(slug, "Status Test")
	require.NoError(t, repo.Create(ctx, p))

	require.NoError(t, repo.UpdateStatus(ctx, slug, domain.StatusStopped, domain.StatusCreating, ""))

	got, err := repo.GetBySlug(ctx, slug)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusStopped, got.Status)
	assert.Equal(t, domain.StatusCreating, got.PreviousStatus)
	assert.Empty(t, got.LastError)
}

func TestIntegration_ProjectRepository_UpdateStatus_NotFound(t *testing.T) {
	pool := testDB(t)
	repo := postgres.NewProjectRepository(pool)
	err := repo.UpdateStatus(context.Background(), "no-such-slug", domain.StatusStopped, "", "")
	assert.ErrorIs(t, err, store.ErrProjectNotFound)
}

func TestIntegration_ProjectRepository_Delete(t *testing.T) {
	pool := testDB(t)
	slug := "int-delete-test"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	p, _ := domain.NewProject(slug, "Delete Test")
	require.NoError(t, repo.Create(ctx, p))
	require.NoError(t, repo.Delete(ctx, slug))

	got, err := repo.GetBySlug(ctx, slug)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDestroyed, got.Status)
}

func TestIntegration_ProjectRepository_Exists(t *testing.T) {
	pool := testDB(t)
	slug := "int-exists-test"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	exists, err := repo.Exists(ctx, slug)
	require.NoError(t, err)
	assert.False(t, exists)

	p, _ := domain.NewProject(slug, "Exists Test")
	require.NoError(t, repo.Create(ctx, p))

	exists, err = repo.Exists(ctx, slug)
	require.NoError(t, err)
	assert.True(t, exists)
}

// ─── ConfigRepository round-trips ────────────────────────────────────────────

func TestIntegration_ConfigRepository_SaveAndGetConfig(t *testing.T) {
	pool := testDB(t)
	slug := "int-config-save"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	projRepo := postgres.NewProjectRepository(pool)
	cfgRepo := postgres.NewConfigRepository(pool)
	ctx := context.Background()

	// Create the project first (FK constraint).
	p, _ := domain.NewProject(slug, "Config Test")
	require.NoError(t, projRepo.Create(ctx, p))

	portSet := &domain.PortSet{
		KongHTTP: 8000, PostgresPort: 5432, PoolerPort: 6543,
		StudioPort: 3000, MetaPort: 8080, ImgProxyPort: 5001,
	}
	cfg, err := domain.ResolveConfig(p, allSecrets(), portSet, nil)
	require.NoError(t, err)

	require.NoError(t, cfgRepo.SaveConfig(ctx, slug, cfg))

	got, err := cfgRepo.GetConfig(ctx, slug)
	require.NoError(t, err)
	assert.Equal(t, slug, got.ProjectSlug)
	// All 94 schema keys should be present.
	assert.Len(t, got.Values, 94, "GetConfig should return all 94 resolved keys")
	assert.Equal(t, "8000", got.Values["KONG_HTTP_PORT"])
}

func TestIntegration_ConfigRepository_GetConfig_NotFound(t *testing.T) {
	pool := testDB(t)
	cfgRepo := postgres.NewConfigRepository(pool)
	_, err := cfgRepo.GetConfig(context.Background(), "no-config-slug-xyz")
	assert.ErrorIs(t, err, store.ErrConfigNotFound)
}

func TestIntegration_ConfigRepository_SaveAndGetOverrides(t *testing.T) {
	pool := testDB(t)
	slug := "int-overrides-test"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	projRepo := postgres.NewProjectRepository(pool)
	cfgRepo := postgres.NewConfigRepository(pool)
	ctx := context.Background()

	p, _ := domain.NewProject(slug, "Overrides Test")
	require.NoError(t, projRepo.Create(ctx, p))

	overrides := map[string]string{
		"DISABLE_SIGNUP": "true",
		"PGRST_DB_SCHEMAS": "public,extensions",
	}
	require.NoError(t, cfgRepo.SaveOverrides(ctx, slug, overrides))

	got, err := cfgRepo.GetOverrides(ctx, slug)
	require.NoError(t, err)
	assert.Equal(t, overrides, got)
}

func TestIntegration_ConfigRepository_SaveOverrides_EmptyClears(t *testing.T) {
	pool := testDB(t)
	slug := "int-overrides-clear"
	cleanupSlug(t, pool, slug)
	t.Cleanup(func() { cleanupSlug(t, pool, slug) })

	projRepo := postgres.NewProjectRepository(pool)
	cfgRepo := postgres.NewConfigRepository(pool)
	ctx := context.Background()

	p, _ := domain.NewProject(slug, "Overrides Clear")
	require.NoError(t, projRepo.Create(ctx, p))

	require.NoError(t, cfgRepo.SaveOverrides(ctx, slug, map[string]string{"DISABLE_SIGNUP": "true"}))
	require.NoError(t, cfgRepo.SaveOverrides(ctx, slug, map[string]string{}))

	got, err := cfgRepo.GetOverrides(ctx, slug)
	require.NoError(t, err)
	assert.Empty(t, got, "overrides should be cleared after SaveOverrides with empty map")
}
