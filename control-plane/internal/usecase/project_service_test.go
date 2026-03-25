package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// newTestService builds a ProjectService with all-happy-path mocks.
func newTestService(t *testing.T, opts ...func(*testServiceOpts)) ProjectService {
	t.Helper()
	o := &testServiceOpts{
		projectRepo:     &mockProjectRepo{},
		configRepo:      &mockConfigRepo{},
		adapter:         &domain.MockRuntimeAdapter{},
		portAllocator:   &mockPortAllocator{},
		secretGenerator: &mockSecretGenerator{},
	}
	for _, opt := range opts {
		opt(o)
	}
	svc, err := NewProjectService(Config{
		ProjectRepo:     o.projectRepo,
		ConfigRepo:      o.configRepo,
		Adapter:         o.adapter,
		PortAllocator:   o.portAllocator,
		SecretGenerator: o.secretGenerator,
	})
	require.NoError(t, err)
	return svc
}

type testServiceOpts struct {
	projectRepo     *mockProjectRepo
	configRepo      *mockConfigRepo
	adapter         *domain.MockRuntimeAdapter
	portAllocator   *mockPortAllocator
	secretGenerator *mockSecretGenerator
}

// stoppedProject returns a mock stopped ProjectModel.
func stoppedProject(slug string) *domain.ProjectModel {
	return &domain.ProjectModel{
		Slug:           slug,
		DisplayName:    "Test Project",
		Status:         domain.StatusStopped,
		PreviousStatus: domain.StatusCreating,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
}

// runningProject returns a mock running ProjectModel.
func runningProject(slug string) *domain.ProjectModel {
	p := stoppedProject(slug)
	p.Status = domain.StatusRunning
	p.PreviousStatus = domain.StatusStarting
	return p
}

// --- NewProjectService validation ---

func TestNewProjectService_MissingDeps(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing ProjectRepo", Config{ConfigRepo: &mockConfigRepo{}, Adapter: &domain.MockRuntimeAdapter{}, PortAllocator: &mockPortAllocator{}, SecretGenerator: &mockSecretGenerator{}}},
		{"missing ConfigRepo", Config{ProjectRepo: &mockProjectRepo{}, Adapter: &domain.MockRuntimeAdapter{}, PortAllocator: &mockPortAllocator{}, SecretGenerator: &mockSecretGenerator{}}},
		{"missing Adapter", Config{ProjectRepo: &mockProjectRepo{}, ConfigRepo: &mockConfigRepo{}, PortAllocator: &mockPortAllocator{}, SecretGenerator: &mockSecretGenerator{}}},
		{"missing PortAllocator", Config{ProjectRepo: &mockProjectRepo{}, ConfigRepo: &mockConfigRepo{}, Adapter: &domain.MockRuntimeAdapter{}, SecretGenerator: &mockSecretGenerator{}}},
		{"missing SecretGenerator", Config{ProjectRepo: &mockProjectRepo{}, ConfigRepo: &mockConfigRepo{}, Adapter: &domain.MockRuntimeAdapter{}, PortAllocator: &mockPortAllocator{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewProjectService(tc.cfg)
			require.Error(t, err)
		})
	}
}

// --- Create ---

func TestCreate_HappyPath(t *testing.T) {
	var createdSlug string
	var savedStatus domain.ProjectStatus

	projRepo := &mockProjectRepo{
		CreateFn: func(_ context.Context, p *domain.ProjectModel) error {
			createdSlug = p.Slug
			return nil
		},
		UpdateStatusFn: func(_ context.Context, slug string, status, _ domain.ProjectStatus, _ string) error {
			savedStatus = status
			return nil
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	view, err := svc.Create(context.Background(), "my-project", "My Project")

	require.NoError(t, err)
	assert.Equal(t, "my-project", createdSlug)
	assert.Equal(t, domain.StatusStopped, savedStatus)
	assert.Equal(t, "my-project", view.Slug)
	assert.Equal(t, string(domain.StatusStopped), view.Status)
}

func TestCreate_InvalidSlug(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(context.Background(), "INVALID SLUG!", "Name")
	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeInvalidInput, ue.Code)
}

func TestCreate_SlugConflict(t *testing.T) {
	projRepo := &mockProjectRepo{
		CreateFn: func(_ context.Context, _ *domain.ProjectModel) error {
			return store.ErrProjectAlreadyExists
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	_, err := svc.Create(context.Background(), "my-project", "My Project")
	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeConflict, ue.Code)
}

func TestCreate_AdapterCreateFailure_SetsError(t *testing.T) {
	var updatedStatus domain.ProjectStatus
	var updatedLastError string

	projRepo := &mockProjectRepo{
		CreateFn: func(_ context.Context, _ *domain.ProjectModel) error { return nil },
		UpdateStatusFn: func(_ context.Context, _ string, status, _ domain.ProjectStatus, lastError string) error {
			updatedStatus = status
			updatedLastError = lastError
			return nil
		},
	}
	adapter := &domain.MockRuntimeAdapter{
		CreateFn: func(_ context.Context, _ *domain.ProjectModel, _ *domain.ProjectConfig) error {
			return errorf("docker create failed")
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) {
		o.projectRepo = projRepo
		o.adapter = adapter
	})
	_, err := svc.Create(context.Background(), "my-project", "My Project")

	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeInternal, ue.Code)
	assert.Equal(t, domain.StatusError, updatedStatus)
	assert.Contains(t, updatedLastError, "docker create failed")
}

// --- List ---

func TestList_HappyPath(t *testing.T) {
	projects := []*domain.ProjectModel{
		stoppedProject("proj-a"),
		stoppedProject("proj-b"),
	}
	projRepo := &mockProjectRepo{
		ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
			return projects, nil
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	views, err := svc.List(context.Background())

	require.NoError(t, err)
	assert.Len(t, views, 2)
	assert.Equal(t, "proj-a", views[0].Slug)
	assert.Nil(t, views[0].Config, "List should not populate Config")
}

// --- Get ---

func TestGet_HappyPath_WithConfig(t *testing.T) {
	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
	}
	configRepo := &mockConfigRepo{
		GetConfigFn: func(_ context.Context, slug string) (*domain.ProjectConfig, error) {
			return &domain.ProjectConfig{
				ProjectSlug: slug,
				Values:      map[string]string{"KONG_HTTP_PORT": "28081", "JWT_SECRET": "secret123"},
				Overrides:   map[string]string{},
			}, nil
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) {
		o.projectRepo = projRepo
		o.configRepo = configRepo
	})
	view, err := svc.Get(context.Background(), "my-project")

	require.NoError(t, err)
	assert.Equal(t, "my-project", view.Slug)
	assert.NotNil(t, view.Config)
	// JWT_SECRET is sensitive — must be masked
	assert.Equal(t, "***", view.Config["JWT_SECRET"])
	// Non-sensitive value is visible
	assert.Equal(t, "28081", view.Config["KONG_HTTP_PORT"])
}

func TestGet_NotFound(t *testing.T) {
	svc := newTestService(t) // default GetBySlug returns ErrProjectNotFound
	_, err := svc.Get(context.Background(), "no-such-project")
	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeNotFound, ue.Code)
}

// --- Start ---

func TestStart_HappyPath(t *testing.T) {
	var savedStatus domain.ProjectStatus

	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, status, _ domain.ProjectStatus, _ string) error {
			savedStatus = status
			return nil
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	view, err := svc.Start(context.Background(), "my-project")

	require.NoError(t, err)
	assert.Equal(t, domain.StatusRunning, savedStatus)
	assert.Equal(t, string(domain.StatusRunning), view.Status)
}

func TestStart_InvalidState_AlreadyRunning(t *testing.T) {
	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return runningProject(slug), nil
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	_, err := svc.Start(context.Background(), "my-project")
	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeInvalidState, ue.Code)
}

func TestStart_AdapterFailure_SetsError(t *testing.T) {
	var finalStatus domain.ProjectStatus

	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, status, _ domain.ProjectStatus, _ string) error {
			finalStatus = status
			return nil
		},
	}
	adapter := &domain.MockRuntimeAdapter{
		StartFn: func(_ context.Context, _ *domain.ProjectModel) error {
			return errorf("compose up failed")
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) {
		o.projectRepo = projRepo
		o.adapter = adapter
	})
	_, err := svc.Start(context.Background(), "my-project")

	require.Error(t, err)
	assert.Equal(t, domain.StatusError, finalStatus)
}

// --- Stop ---

func TestStop_HappyPath(t *testing.T) {
	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return runningProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, _ domain.ProjectStatus, _ domain.ProjectStatus, _ string) error {
			return nil
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	view, err := svc.Stop(context.Background(), "my-project")

	require.NoError(t, err)
	assert.Equal(t, string(domain.StatusStopped), view.Status)
}

func TestStop_InvalidState_AlreadyStopped(t *testing.T) {
	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	_, err := svc.Stop(context.Background(), "my-project")
	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeInvalidState, ue.Code)
}

// --- Delete ---

func TestDelete_HappyPath(t *testing.T) {
	var deletedSlug string
	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, _ domain.ProjectStatus, _ domain.ProjectStatus, _ string) error {
			return nil
		},
		DeleteFn: func(_ context.Context, slug string) error {
			deletedSlug = slug
			return nil
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	view, err := svc.Delete(context.Background(), "my-project")

	require.NoError(t, err)
	assert.Equal(t, "my-project", deletedSlug)
	assert.Equal(t, string(domain.StatusDestroyed), view.Status)
}

func TestDelete_InvalidState_Running(t *testing.T) {
	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return runningProject(slug), nil
		},
	}
	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	_, err := svc.Delete(context.Background(), "my-project")
	require.Error(t, err)
	var ue *UsecaseError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, ErrCodeInvalidState, ue.Code)
}

// --- Reset ---

func TestReset_HappyPath_FromStopped(t *testing.T) {
	var statusSequence []domain.ProjectStatus

	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, status, _ domain.ProjectStatus, _ string) error {
			statusSequence = append(statusSequence, status)
			return nil
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	view, err := svc.Reset(context.Background(), "my-project")

	require.NoError(t, err)
	assert.Equal(t, string(domain.StatusRunning), view.Status)
	// Expected sequence: destroying → creating → running
	assert.Equal(t, []domain.ProjectStatus{
		domain.StatusDestroying,
		domain.StatusCreating,
		domain.StatusRunning,
	}, statusSequence)
}

func TestReset_HappyPath_FromRunning(t *testing.T) {
	var statusSequence []domain.ProjectStatus

	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return runningProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, status, _ domain.ProjectStatus, _ string) error {
			statusSequence = append(statusSequence, status)
			return nil
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) { o.projectRepo = projRepo })
	_, err := svc.Reset(context.Background(), "my-project")

	require.NoError(t, err)
	// Expected: stopping → stopped → destroying → creating → running
	assert.Contains(t, statusSequence, domain.StatusStopping)
	assert.Contains(t, statusSequence, domain.StatusDestroying)
	assert.Contains(t, statusSequence, domain.StatusRunning)
}

func TestReset_DestroyFailure_SetsError(t *testing.T) {
	var finalStatus domain.ProjectStatus

	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, status, _ domain.ProjectStatus, _ string) error {
			finalStatus = status
			return nil
		},
	}
	adapter := &domain.MockRuntimeAdapter{
		DestroyFn: func(_ context.Context, _ *domain.ProjectModel) error {
			return errorf("destroy failed")
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) {
		o.projectRepo = projRepo
		o.adapter = adapter
	})
	_, err := svc.Reset(context.Background(), "my-project")

	require.Error(t, err)
	assert.Equal(t, domain.StatusError, finalStatus)
}

func TestReset_StartFailure_CleanupAttempted(t *testing.T) {
	var destroyCalled int

	projRepo := &mockProjectRepo{
		GetBySlugFn: func(_ context.Context, slug string) (*domain.ProjectModel, error) {
			return stoppedProject(slug), nil
		},
		UpdateStatusFn: func(_ context.Context, _ string, _ domain.ProjectStatus, _ domain.ProjectStatus, _ string) error {
			return nil
		},
	}
	adapter := &domain.MockRuntimeAdapter{
		DestroyFn: func(_ context.Context, _ *domain.ProjectModel) error {
			destroyCalled++
			return nil // first call succeeds, second (cleanup) also succeeds
		},
		StartFn: func(_ context.Context, _ *domain.ProjectModel) error {
			return errorf("start failed")
		},
	}

	svc := newTestService(t, func(o *testServiceOpts) {
		o.projectRepo = projRepo
		o.adapter = adapter
	})
	_, err := svc.Reset(context.Background(), "my-project")

	require.Error(t, err)
	// Destroy should be called twice: once for actual destroy, once for cleanup
	assert.Equal(t, 2, destroyCalled, "Destroy should be called for cleanup after Start failure")
}

// --- UsecaseError ---

func TestUsecaseError_ErrorMethod(t *testing.T) {
	t.Run("with wrapped error", func(t *testing.T) {
		cause := errors.New("db down")
		ue := &UsecaseError{Code: ErrCodeInternal, Message: "failed", Err: cause}
		assert.Contains(t, ue.Error(), "[internal]")
		assert.Contains(t, ue.Error(), "failed")
		assert.Contains(t, ue.Error(), "db down")
	})
	t.Run("without wrapped error", func(t *testing.T) {
		ue := &UsecaseError{Code: ErrCodeNotFound, Message: "not found"}
		assert.Equal(t, "[not_found] not found", ue.Error())
	})
}

func TestUsecaseError_Unwrap(t *testing.T) {
	cause := errors.New("original")
	ue := &UsecaseError{Code: ErrCodeInternal, Message: "wrapped", Err: cause}
	assert.True(t, errors.Is(ue, cause))
}

// --- toProjectView ---

func TestToProjectView_MasksConfig(t *testing.T) {
	p := stoppedProject("test-proj")
	config := &domain.ProjectConfig{
		ProjectSlug: "test-proj",
		Values: map[string]string{
			"KONG_HTTP_PORT": "28081",
			"JWT_SECRET":     "supersecret",
		},
		Overrides: map[string]string{},
	}

	view := toProjectView(p, config, nil)
	assert.Equal(t, "28081", view.Config["KONG_HTTP_PORT"])
	assert.Equal(t, "***", view.Config["JWT_SECRET"], "sensitive fields must be masked")
}

func TestToProjectView_BuildsURLs(t *testing.T) {
	p := stoppedProject("test-proj")
	config := &domain.ProjectConfig{
		ProjectSlug: "test-proj",
		Values: map[string]string{
			"KONG_HTTP_PORT": "28081",
			"STUDIO_PORT":    "54323",
		},
		Overrides: map[string]string{},
	}

	view := toProjectView(p, config, nil)
	require.NotNil(t, view.URLs)
	assert.Equal(t, "http://localhost:28081", view.URLs.API)
	assert.Equal(t, "http://localhost:54323", view.URLs.Studio)
}

func TestToProjectView_NilConfig_NoURLs(t *testing.T) {
	p := stoppedProject("test-proj")
	view := toProjectView(p, nil, nil)
	assert.Nil(t, view.Config)
	assert.Nil(t, view.URLs)
}
