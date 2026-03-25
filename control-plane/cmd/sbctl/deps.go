package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kevin/supabase-control-plane/internal/adapter/compose"
	"github.com/kevin/supabase-control-plane/internal/domain"
	storepostgres "github.com/kevin/supabase-control-plane/internal/store/postgres"
	"github.com/kevin/supabase-control-plane/internal/usecase"
)

// Deps holds all initialised dependencies for a single CLI invocation.
// It is constructed once in PersistentPreRunE and shared via closure capture.
type Deps struct {
	ProjectService usecase.ProjectService
}

// BuildDeps constructs all dependencies from the given configuration.
// Returns an error if the database is unreachable or any initialisation fails.
func BuildDeps(ctx context.Context, dbURL, projectsDir string) (*Deps, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	projectRepo := storepostgres.NewProjectRepository(pool)
	configRepo := storepostgres.NewConfigRepository(pool)

	renderer := compose.NewComposeEnvRenderer()
	adapter := compose.NewComposeAdapter(projectsDir, renderer)
	allocator := compose.NewComposePortAllocator(projectRepo, configRepo)
	secretGen := domain.NewSecretGenerator()

	svc, err := usecase.NewProjectService(usecase.Config{
		ProjectRepo:     projectRepo,
		ConfigRepo:      configRepo,
		Adapter:         adapter,
		PortAllocator:   allocator,
		SecretGenerator: secretGen,
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("initialise project service: %w", err)
	}

	return &Deps{ProjectService: svc}, nil
}
