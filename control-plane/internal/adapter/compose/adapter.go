package compose

import (
	"context"
	"os"
	"path/filepath"
	"time"

	_ "embed"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

//go:embed templates/docker-compose.yml
var composeTemplate []byte

// ComposeAdapter implements domain.RuntimeAdapter using Docker Compose v2.
//
// projectsDir is the base directory; each project occupies a subdirectory
// named by its slug:
//
//	<projectsDir>/<slug>/
//	  docker-compose.yml  ← embedded template, written once at Create time
//	  .env                ← rendered by ConfigRenderer, rewritten at ApplyConfig
type ComposeAdapter struct {
	projectsDir string
	renderer    domain.ConfigRenderer
	runner      cmdRunner
}

// Static interface assertion.
var _ domain.RuntimeAdapter = (*ComposeAdapter)(nil)

// NewComposeAdapter returns a ComposeAdapter backed by the OS command runner.
func NewComposeAdapter(projectsDir string, renderer domain.ConfigRenderer) *ComposeAdapter {
	return newComposeAdapterWithRunner(projectsDir, renderer, &osCmdRunner{})
}

// newComposeAdapterWithRunner is the white-box constructor used in tests.
func newComposeAdapterWithRunner(projectsDir string, renderer domain.ConfigRenderer, runner cmdRunner) *ComposeAdapter {
	return &ComposeAdapter{
		projectsDir: projectsDir,
		renderer:    renderer,
		runner:      runner,
	}
}

// projectDir returns the absolute path to a project's directory.
func (a *ComposeAdapter) projectDir(slug string) string {
	return filepath.Join(a.projectsDir, slug)
}

// Create renders config artifacts and writes them along with the embedded
// docker-compose.yml to the project directory.
//
// renderer.Render is called before any filesystem mutation so that a render
// failure does not leave a partially-created directory behind.
//
// Does NOT start containers — that is the responsibility of Start.
func (a *ComposeAdapter) Create(ctx context.Context, project *domain.ProjectModel, config *domain.ProjectConfig) error {
	dir := a.projectDir(project.Slug)

	// Render first (fail-fast before any filesystem mutation).
	artifacts, err := a.renderer.Render(config)
	if err != nil {
		return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
	}

	for _, art := range artifacts {
		dst := filepath.Join(dir, art.Path)
		if err := os.WriteFile(dst, art.Content, os.FileMode(art.Mode)); err != nil {
			return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), composeTemplate, 0644); err != nil {
		return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
	}

	return nil
}

// Start runs `docker compose up -d` and polls Status every 5 seconds for up to
// 120 seconds until all services are healthy. Returns a *domain.StartError
// containing a health snapshot if services do not become healthy in time.
func (a *ComposeAdapter) Start(ctx context.Context, project *domain.ProjectModel) error {
	dir := a.projectDir(project.Slug)

	if _, err := a.runner.Run(ctx, dir, "docker", "compose", "up", "-d"); err != nil {
		return &domain.StartError{Slug: project.Slug, Err: err}
	}

	const (
		pollInterval = 5 * time.Second
		maxTicks     = 24 // 24 × 5s = 120s
	)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastHealth *domain.ProjectHealth
	ticks := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ticks++
			h, err := a.Status(ctx, project)
			if err == nil && h.IsHealthy() {
				return nil
			}
			lastHealth = h
			if ticks >= maxTicks {
				return &domain.StartError{
					Slug:   project.Slug,
					Err:    domain.ErrServiceNotHealthy,
					Health: lastHealth,
				}
			}
		}
	}
}

// Stop runs `docker compose stop` to gracefully stop all containers.
func (a *ComposeAdapter) Stop(ctx context.Context, project *domain.ProjectModel) error {
	dir := a.projectDir(project.Slug)
	if _, err := a.runner.Run(ctx, dir, "docker", "compose", "stop"); err != nil {
		return &domain.AdapterError{Operation: "stop", Slug: project.Slug, Err: err}
	}
	return nil
}

// Destroy runs `docker compose down -v --remove-orphans` to remove containers
// and volumes, then removes the project directory. Both steps are always
// attempted (best-effort cleanup) — downErr takes priority if both fail.
func (a *ComposeAdapter) Destroy(ctx context.Context, project *domain.ProjectModel) error {
	dir := a.projectDir(project.Slug)

	_, downErr := a.runner.Run(ctx, dir, "docker", "compose", "down", "-v", "--remove-orphans")
	cleanupErr := os.RemoveAll(dir)

	if downErr != nil {
		return &domain.AdapterError{Operation: "destroy", Slug: project.Slug, Err: downErr}
	}
	if cleanupErr != nil {
		return &domain.AdapterError{Operation: "destroy:cleanup", Slug: project.Slug, Err: cleanupErr}
	}
	return nil
}

// Status runs `docker compose ps --format json` and returns a health snapshot.
func (a *ComposeAdapter) Status(ctx context.Context, project *domain.ProjectModel) (*domain.ProjectHealth, error) {
	dir := a.projectDir(project.Slug)
	out, err := a.runner.Run(ctx, dir, "docker", "compose", "ps", "--format", "json")
	if err != nil {
		return nil, &domain.AdapterError{Operation: "status", Slug: project.Slug, Err: err}
	}
	return parseComposePS(out), nil
}

// RenderConfig is a pure computation that delegates to the renderer.
// It does not write to disk.
func (a *ComposeAdapter) RenderConfig(_ context.Context, _ *domain.ProjectModel, config *domain.ProjectConfig) ([]domain.Artifact, error) {
	return a.renderer.Render(config)
}

// ApplyConfig re-renders config artifacts, writes them to the project directory,
// and — if the project is currently running — reconciles containers via
// `docker compose up -d`.
func (a *ComposeAdapter) ApplyConfig(ctx context.Context, project *domain.ProjectModel, config *domain.ProjectConfig) error {
	dir := a.projectDir(project.Slug)

	artifacts, err := a.renderer.Render(config)
	if err != nil {
		return &domain.AdapterError{Operation: "apply-config", Slug: project.Slug, Err: err}
	}

	for _, art := range artifacts {
		dst := filepath.Join(dir, art.Path)
		if err := os.WriteFile(dst, art.Content, os.FileMode(art.Mode)); err != nil {
			return &domain.AdapterError{Operation: "apply-config", Slug: project.Slug, Err: err}
		}
	}

	if project.Status == domain.StatusRunning {
		if _, err := a.runner.Run(ctx, dir, "docker", "compose", "up", "-d"); err != nil {
			return &domain.AdapterError{Operation: "apply-config:reconcile", Slug: project.Slug, Err: err}
		}
	}

	return nil
}


