package compose

import (
	"context"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// ── Mock repositories for ComposePortAllocator tests ──────────────────────────

type mockProjectRepo struct {
	ListFn func(ctx context.Context, filters ...store.ListFilter) ([]*domain.ProjectModel, error)
}

func (m *mockProjectRepo) List(ctx context.Context, f ...store.ListFilter) ([]*domain.ProjectModel, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, f...)
	}
	return nil, nil
}

func (m *mockProjectRepo) Create(_ context.Context, _ *domain.ProjectModel) error { return nil }
func (m *mockProjectRepo) GetBySlug(_ context.Context, _ string) (*domain.ProjectModel, error) {
	return nil, nil
}
func (m *mockProjectRepo) UpdateStatus(_ context.Context, _ string, _, _ domain.ProjectStatus, _ string) error {
	return nil
}
func (m *mockProjectRepo) Delete(_ context.Context, _ string) error { return nil }
func (m *mockProjectRepo) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

type mockConfigRepo struct {
	GetConfigFn func(ctx context.Context, slug string) (*domain.ProjectConfig, error)
}

func (m *mockConfigRepo) GetConfig(ctx context.Context, slug string) (*domain.ProjectConfig, error) {
	if m.GetConfigFn != nil {
		return m.GetConfigFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockConfigRepo) SaveConfig(_ context.Context, _ string, _ *domain.ProjectConfig) error {
	return nil
}
func (m *mockConfigRepo) SaveOverrides(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (m *mockConfigRepo) GetOverrides(_ context.Context, _ string) (map[string]string, error) {
	return nil, nil
}
func (m *mockConfigRepo) DeleteConfig(_ context.Context, _ string) error { return nil }

// ── Mock ConfigRenderer for ComposeAdapter tests ───────────────────────────────

type mockConfigRenderer struct {
	RenderFn func(config *domain.ProjectConfig) ([]domain.Artifact, error)
}

func (m *mockConfigRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error) {
	if m.RenderFn != nil {
		return m.RenderFn(config)
	}
	return []domain.Artifact{{Path: ".env", Content: []byte("KEY=val\n"), Mode: 0600}}, nil
}

// ── Fake cmdRunner for ComposeAdapter tests ────────────────────────────────────

type fakeCall struct {
	Dir  string
	Name string
	Args []string
}

type fakeCmdRunner struct {
	RunFn func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	Calls []fakeCall
}

func (f *fakeCmdRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	f.Calls = append(f.Calls, fakeCall{Dir: dir, Name: name, Args: args})
	if f.RunFn != nil {
		return f.RunFn(ctx, dir, name, args...)
	}
	return nil, nil
}
