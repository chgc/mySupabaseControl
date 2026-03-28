package k8s

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// ── Mock repositories ─────────────────────────────────────────────────────────

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
func (m *mockProjectRepo) Delete(_ context.Context, _ string) error         { return nil }
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

// ── Helpers ───────────────────────────────────────────────────────────────────

func makeConfig(kongHTTP, postgres int) *domain.ProjectConfig {
	return &domain.ProjectConfig{
		Values: map[string]string{
			"KONG_HTTP_PORT":                fmt.Sprintf("%d", kongHTTP),
			"POSTGRES_PORT":                 fmt.Sprintf("%d", postgres),
			"POOLER_PROXY_PORT_TRANSACTION": "0",
		},
	}
}

func k8sProject(slug string) *domain.ProjectModel {
	return &domain.ProjectModel{
		Slug:        slug,
		RuntimeType: domain.RuntimeKubernetes,
	}
}

func composeProject(slug string) *domain.ProjectModel {
	return &domain.ProjectModel{
		Slug:        slug,
		RuntimeType: domain.RuntimeDockerCompose,
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestAllocatePorts_NoExistingProjects(t *testing.T) {
	alloc := NewK8sPortAllocator(&mockProjectRepo{}, &mockConfigRepo{})

	ps, err := alloc.AllocatePorts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps.KongHTTP != 30080 {
		t.Errorf("KongHTTP = %d, want 30080", ps.KongHTTP)
	}
	if ps.PostgresPort != 30432 {
		t.Errorf("PostgresPort = %d, want 30432", ps.PostgresPort)
	}
	if ps.PoolerPort != 0 {
		t.Errorf("PoolerPort = %d, want 0", ps.PoolerPort)
	}
}

func TestAllocatePorts_WithExistingProjects(t *testing.T) {
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			return []*domain.ProjectModel{k8sProject("proj-a")}, nil
		},
	}
	cfgRepo := &mockConfigRepo{
		GetConfigFn: func(_ context.Context, slug string) (*domain.ProjectConfig, error) {
			if slug == "proj-a" {
				return makeConfig(30080, 30432), nil
			}
			return nil, store.ErrConfigNotFound
		},
	}
	alloc := NewK8sPortAllocator(projRepo, cfgRepo)

	ps, err := alloc.AllocatePorts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps.KongHTTP != 30081 {
		t.Errorf("KongHTTP = %d, want 30081", ps.KongHTTP)
	}
	if ps.PostgresPort != 30433 {
		t.Errorf("PostgresPort = %d, want 30433", ps.PostgresPort)
	}
	if ps.PoolerPort != 0 {
		t.Errorf("PoolerPort = %d, want 0", ps.PoolerPort)
	}
}

func TestAllocatePorts_OnlyCountsK8sProjects(t *testing.T) {
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			return []*domain.ProjectModel{
				composeProject("compose-proj"),
				k8sProject("k8s-proj"),
			}, nil
		},
	}
	cfgRepo := &mockConfigRepo{
		GetConfigFn: func(_ context.Context, slug string) (*domain.ProjectConfig, error) {
			switch slug {
			case "compose-proj":
				// Should never be called for compose projects.
				t.Errorf("GetConfig called for compose project %q", slug)
				return makeConfig(30080, 30432), nil
			case "k8s-proj":
				return makeConfig(30085, 30440), nil
			}
			return nil, store.ErrConfigNotFound
		},
	}
	alloc := NewK8sPortAllocator(projRepo, cfgRepo)

	ps, err := alloc.AllocatePorts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Base ports should be returned since compose project ports are ignored
	// and k8s-proj uses 30085/30440 which don't conflict with bases.
	if ps.KongHTTP != 30080 {
		t.Errorf("KongHTTP = %d, want 30080", ps.KongHTTP)
	}
	if ps.PostgresPort != 30432 {
		t.Errorf("PostgresPort = %d, want 30432", ps.PostgresPort)
	}
}

func TestAllocatePorts_ExhaustedRange(t *testing.T) {
	// Fill all ports from 30080 to 32767.
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			var projects []*domain.ProjectModel
			for i := 30080; i <= 32767; i++ {
				projects = append(projects, k8sProject(fmt.Sprintf("proj-%d", i)))
			}
			return projects, nil
		},
	}
	portIdx := 30080
	cfgRepo := &mockConfigRepo{
		GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
			p := portIdx
			portIdx++
			return makeConfig(p, p), nil
		},
	}
	alloc := NewK8sPortAllocator(projRepo, cfgRepo)

	_, err := alloc.AllocatePorts(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNoAvailablePort) {
		t.Errorf("error = %v, want ErrNoAvailablePort", err)
	}
}

func TestAllocatePorts_RepoError(t *testing.T) {
	repoErr := errors.New("database down")
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			return nil, repoErr
		},
	}
	alloc := NewK8sPortAllocator(projRepo, &mockConfigRepo{})

	_, err := alloc.AllocatePorts(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("error = %v, want wrapped %v", err, repoErr)
	}
}

func TestAllocatePorts_InvalidPortString(t *testing.T) {
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			return []*domain.ProjectModel{k8sProject("bad-cfg")}, nil
		},
	}
	cfgRepo := &mockConfigRepo{
		GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
			return &domain.ProjectConfig{
				Values: map[string]string{
					"KONG_HTTP_PORT":                "not-a-number",
					"POSTGRES_PORT":                 "xyz",
					"POOLER_PROXY_PORT_TRANSACTION": "0",
				},
			}, nil
		},
	}
	alloc := NewK8sPortAllocator(projRepo, cfgRepo)

	ps, err := alloc.AllocatePorts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps.KongHTTP != 30080 {
		t.Errorf("KongHTTP = %d, want 30080", ps.KongHTTP)
	}
	if ps.PostgresPort != 30432 {
		t.Errorf("PostgresPort = %d, want 30432", ps.PostgresPort)
	}
}

func TestAllocatePorts_ConcurrentSafety(t *testing.T) {
	projRepo := &mockProjectRepo{}
	cfgRepo := &mockConfigRepo{}
	alloc := NewK8sPortAllocator(projRepo, cfgRepo)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := alloc.AllocatePorts(context.Background())
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error from goroutine: %v", err)
	}
}

func TestAllocatePorts_ConfigNotFound(t *testing.T) {
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			return []*domain.ProjectModel{k8sProject("no-config")}, nil
		},
	}
	cfgRepo := &mockConfigRepo{
		GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
			return nil, store.ErrConfigNotFound
		},
	}
	alloc := NewK8sPortAllocator(projRepo, cfgRepo)

	ps, err := alloc.AllocatePorts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps.KongHTTP != 30080 {
		t.Errorf("KongHTTP = %d, want 30080", ps.KongHTTP)
	}
	if ps.PostgresPort != 30432 {
		t.Errorf("PostgresPort = %d, want 30432", ps.PostgresPort)
	}
	if ps.PoolerPort != 0 {
		t.Errorf("PoolerPort = %d, want 0", ps.PoolerPort)
	}
}
