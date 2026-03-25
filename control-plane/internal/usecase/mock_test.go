package usecase

import (
	"context"
	"errors"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// --- mockProjectRepo ---

type mockProjectRepo struct {
	CreateFn       func(ctx context.Context, project *domain.ProjectModel) error
	GetBySlugFn    func(ctx context.Context, slug string) (*domain.ProjectModel, error)
	ListFn         func(ctx context.Context, filters ...store.ListFilter) ([]*domain.ProjectModel, error)
	UpdateStatusFn func(ctx context.Context, slug string, status, previousStatus domain.ProjectStatus, lastError string) error
	DeleteFn       func(ctx context.Context, slug string) error
	ExistsFn       func(ctx context.Context, slug string) (bool, error)
}

func (m *mockProjectRepo) Create(ctx context.Context, project *domain.ProjectModel) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, project)
	}
	return nil
}

func (m *mockProjectRepo) GetBySlug(ctx context.Context, slug string) (*domain.ProjectModel, error) {
	if m.GetBySlugFn != nil {
		return m.GetBySlugFn(ctx, slug)
	}
	return nil, store.ErrProjectNotFound
}

func (m *mockProjectRepo) List(ctx context.Context, filters ...store.ListFilter) ([]*domain.ProjectModel, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, filters...)
	}
	return nil, nil
}

func (m *mockProjectRepo) UpdateStatus(ctx context.Context, slug string, status, previousStatus domain.ProjectStatus, lastError string) error {
	if m.UpdateStatusFn != nil {
		return m.UpdateStatusFn(ctx, slug, status, previousStatus, lastError)
	}
	return nil
}

func (m *mockProjectRepo) Delete(ctx context.Context, slug string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, slug)
	}
	return nil
}

func (m *mockProjectRepo) Exists(ctx context.Context, slug string) (bool, error) {
	if m.ExistsFn != nil {
		return m.ExistsFn(ctx, slug)
	}
	return false, nil
}

// --- mockConfigRepo ---

type mockConfigRepo struct {
	SaveConfigFn    func(ctx context.Context, projectSlug string, config *domain.ProjectConfig) error
	GetConfigFn     func(ctx context.Context, projectSlug string) (*domain.ProjectConfig, error)
	SaveOverridesFn func(ctx context.Context, projectSlug string, overrides map[string]string) error
	GetOverridesFn  func(ctx context.Context, projectSlug string) (map[string]string, error)
	DeleteConfigFn  func(ctx context.Context, projectSlug string) error
}

func (m *mockConfigRepo) SaveConfig(ctx context.Context, projectSlug string, config *domain.ProjectConfig) error {
	if m.SaveConfigFn != nil {
		return m.SaveConfigFn(ctx, projectSlug, config)
	}
	return nil
}

func (m *mockConfigRepo) GetConfig(ctx context.Context, projectSlug string) (*domain.ProjectConfig, error) {
	if m.GetConfigFn != nil {
		return m.GetConfigFn(ctx, projectSlug)
	}
	return &domain.ProjectConfig{ProjectSlug: projectSlug, Values: map[string]string{}, Overrides: map[string]string{}}, nil
}

func (m *mockConfigRepo) SaveOverrides(ctx context.Context, projectSlug string, overrides map[string]string) error {
	if m.SaveOverridesFn != nil {
		return m.SaveOverridesFn(ctx, projectSlug, overrides)
	}
	return nil
}

func (m *mockConfigRepo) GetOverrides(ctx context.Context, projectSlug string) (map[string]string, error) {
	if m.GetOverridesFn != nil {
		return m.GetOverridesFn(ctx, projectSlug)
	}
	return map[string]string{}, nil
}

func (m *mockConfigRepo) DeleteConfig(ctx context.Context, projectSlug string) error {
	if m.DeleteConfigFn != nil {
		return m.DeleteConfigFn(ctx, projectSlug)
	}
	return nil
}

// --- mockPortAllocator ---

type mockPortAllocator struct {
	AllocatePortsFn func(ctx context.Context) (*domain.PortSet, error)
}

func (m *mockPortAllocator) AllocatePorts(ctx context.Context) (*domain.PortSet, error) {
	if m.AllocatePortsFn != nil {
		return m.AllocatePortsFn(ctx)
	}
	return &domain.PortSet{
		KongHTTP:     28081,
		PostgresPort: 54320,
		PoolerPort:   64300,
		StudioPort:   54323,
		MetaPort:     54380,
		ImgProxyPort: 54381,
	}, nil
}

// --- mockSecretGenerator ---

type mockSecretGenerator struct {
	RandomHexFn         func(length int) (string, error)
	RandomAlphanumericFn func(length int) (string, error)
	GenerateJWTFn       func(secret string, role string, expiry int) (string, error)
}

func (m *mockSecretGenerator) RandomHex(length int) (string, error) {
	if m.RandomHexFn != nil {
		return m.RandomHexFn(length)
	}
	return "aabbccddeeff0011223344556677889900112233445566778899aabbccddeeff", nil
}

func (m *mockSecretGenerator) RandomAlphanumeric(length int) (string, error) {
	if m.RandomAlphanumericFn != nil {
		return m.RandomAlphanumericFn(length)
	}
	return "abcdefghijklmnopqrstuvwxyz012345", nil
}

func (m *mockSecretGenerator) GenerateJWT(secret string, role string, expiry int) (string, error) {
	if m.GenerateJWTFn != nil {
		return m.GenerateJWTFn(secret, role, expiry)
	}
	return "mock.jwt.token", nil
}

// --- helpers ---

// errorf is a convenience to build a simple sentinel error for tests.
func errorf(msg string) error { return errors.New(msg) }
