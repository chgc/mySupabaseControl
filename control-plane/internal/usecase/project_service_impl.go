package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// ProjectService defines all use-case operations for managing Supabase projects.
// It is the single entry point for CLI, MCP Server, and Telegram Bot.
type ProjectService interface {
	// Create provisions a new project: allocates ports, generates secrets,
	// persists the project record, and creates runtime resources.
	// Terminates in stopped state. Does NOT start the project.
	// Returns ErrCodeConflict if the slug already exists.
	// Returns ErrCodeInvalidInput if the slug or displayName fails validation.
	Create(ctx context.Context, slug, displayName string) (*ProjectView, error)

	// List returns all active projects (excluding destroyed ones).
	// Health and Config fields are not populated.
	List(ctx context.Context) ([]*ProjectView, error)

	// Get returns a single project by slug with masked config and live health status.
	// Returns ErrCodeNotFound if the project does not exist or is destroyed.
	Get(ctx context.Context, slug string) (*ProjectView, error)

	// Start brings up all Docker Compose services for the project.
	// Returns ErrCodeInvalidState if the project is not in a startable state.
	Start(ctx context.Context, slug string) (*ProjectView, error)

	// Stop tears down all Docker Compose services while preserving data.
	// Returns ErrCodeInvalidState if the project is not running.
	Stop(ctx context.Context, slug string) (*ProjectView, error)

	// Reset performs a full data wipe and re-provision:
	//   Stop (if running) → Destroy → Create → Start
	// After Reset the project is in running state with a clean database.
	// Returns ErrCodeNotFound if the project does not exist.
	Reset(ctx context.Context, slug string) (*ProjectView, error)

	// Delete destroys all runtime resources and soft-deletes the project record.
	// Config rows are retained for audit purposes (status = destroyed).
	// Returns ErrCodeInvalidState if the project is not in a destroyable state.
	Delete(ctx context.Context, slug string) (*ProjectView, error)

	// GetCredentials returns the project's admin credentials in plain text,
	// including dashboard password, API keys, and direct database access info.
	// Returns ErrCodeNotFound if the project does not exist or is destroyed.
	GetCredentials(ctx context.Context, slug string) (*CredentialsView, error)
}

// NewProjectService constructs a ProjectService with all dependencies.
// Returns an error if any required field in cfg is nil/zero.
func NewProjectService(cfg Config) (ProjectService, error) {
	if cfg.ProjectRepo == nil {
		return nil, fmt.Errorf("usecase: Config.ProjectRepo is required")
	}
	if cfg.ConfigRepo == nil {
		return nil, fmt.Errorf("usecase: Config.ConfigRepo is required")
	}
	if cfg.Adapter == nil {
		return nil, fmt.Errorf("usecase: Config.Adapter is required")
	}
	if cfg.PortAllocator == nil {
		return nil, fmt.Errorf("usecase: Config.PortAllocator is required")
	}
	if cfg.SecretGenerator == nil {
		return nil, fmt.Errorf("usecase: Config.SecretGenerator is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &projectService{
		projectRepo:     cfg.ProjectRepo,
		configRepo:      cfg.ConfigRepo,
		adapter:         cfg.Adapter,
		portAllocator:   cfg.PortAllocator,
		secretGenerator: cfg.SecretGenerator,
		log:             logger,
	}, nil
}

type projectService struct {
	projectRepo     store.ProjectRepository
	configRepo      store.ConfigRepository
	adapter         domain.RuntimeAdapter
	portAllocator   domain.PortAllocator
	secretGenerator domain.SecretGenerator
	log             *slog.Logger
}

// --- Create ---

func (s *projectService) Create(ctx context.Context, slug, displayName string) (*ProjectView, error) {
	if err := domain.ValidateSlug(slug); err != nil {
		return nil, &UsecaseError{Code: ErrCodeInvalidInput, Message: err.Error(), Err: err}
	}

	project, err := domain.NewProject(slug, displayName, domain.RuntimeDockerCompose)
	if err != nil {
		return nil, &UsecaseError{Code: ErrCodeInvalidInput, Message: err.Error(), Err: err}
	}

	if err := s.projectRepo.Create(ctx, project); err != nil {
		if errors.Is(err, store.ErrProjectAlreadyExists) {
			return nil, &UsecaseError{Code: ErrCodeConflict, Message: fmt.Sprintf("project %q already exists", slug), Err: err}
		}
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to create project record", Err: err}
	}

	// From this point, project row exists — failures must SetError.
	config, provErr := s.provisionConfig(ctx, project, nil)
	if provErr != nil {
		s.setErrorAndLog(ctx, project, provErr.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to provision config", Err: provErr}
	}

	if err := s.adapter.Create(ctx, project, config); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to create runtime resources", Err: err}
	}

	// domain.NewProject sets status=creating; transition to stopped on success.
	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusStopped, domain.StatusCreating, ""); err != nil {
		s.log.Error("orphaned_state: failed to update status after create",
			"slug", slug, "error", err)
	} else {
		project.Status = domain.StatusStopped
		project.PreviousStatus = domain.StatusCreating
	}

	return toProjectView(project, config, nil), nil
}

// --- List ---

func (s *projectService) List(ctx context.Context) ([]*ProjectView, error) {
	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to list projects", Err: err}
	}
	views := make([]*ProjectView, 0, len(projects))
	for _, p := range projects {
		views = append(views, toProjectView(p, nil, nil))
	}
	return views, nil
}

// --- Get ---

func (s *projectService) Get(ctx context.Context, slug string) (*ProjectView, error) {
	project, err := s.projectRepo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, mapNotFoundErr(slug, err)
	}

	config, err := s.configRepo.GetConfig(ctx, slug)
	if err != nil && !errors.Is(err, store.ErrConfigNotFound) {
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to load config", Err: err}
	}

	var health *domain.ProjectHealth
	if project.Status == domain.StatusRunning {
		health, _ = s.adapter.Status(ctx, project) // best-effort
	}

	return toProjectView(project, config, health), nil
}

// --- Start ---

func (s *projectService) Start(ctx context.Context, slug string) (*ProjectView, error) {
	project, err := s.projectRepo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, mapNotFoundErr(slug, err)
	}
	if !project.CanStart() {
		return nil, &UsecaseError{
			Code:    ErrCodeInvalidState,
			Message: fmt.Sprintf("project %q cannot be started from status %q", slug, project.Status),
		}
	}

	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusStarting, project.Status, ""); err != nil {
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to update status", Err: err}
	}
	project.PreviousStatus = project.Status
	project.Status = domain.StatusStarting

	if err := s.adapter.Start(ctx, project); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to start project", Err: err}
	}

	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusRunning, domain.StatusStarting, ""); err != nil {
		s.log.Error("orphaned_state: failed to update status after start",
			"slug", slug, "error", err)
	} else {
		project.PreviousStatus = domain.StatusStarting
		project.Status = domain.StatusRunning
	}

	health, _ := s.adapter.Status(ctx, project) // best-effort
	return toProjectView(project, nil, health), nil
}

// --- Stop ---

func (s *projectService) Stop(ctx context.Context, slug string) (*ProjectView, error) {
	project, err := s.projectRepo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, mapNotFoundErr(slug, err)
	}
	if !project.CanStop() {
		return nil, &UsecaseError{
			Code:    ErrCodeInvalidState,
			Message: fmt.Sprintf("project %q cannot be stopped from status %q", slug, project.Status),
		}
	}

	if err := s.stopProject(ctx, project); err != nil {
		return nil, err
	}
	return toProjectView(project, nil, nil), nil
}

// stopProject is an internal helper used by Stop and Reset.
// It mutates project.Status in-place on success.
func (s *projectService) stopProject(ctx context.Context, project *domain.ProjectModel) error {
	prev := project.Status
	if err := s.projectRepo.UpdateStatus(ctx, project.Slug, domain.StatusStopping, prev, ""); err != nil {
		return &UsecaseError{Code: ErrCodeInternal, Message: "failed to update status", Err: err}
	}
	project.PreviousStatus = prev
	project.Status = domain.StatusStopping

	if err := s.adapter.Stop(ctx, project); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return &UsecaseError{Code: ErrCodeInternal, Message: "failed to stop project", Err: err}
	}

	if err := s.projectRepo.UpdateStatus(ctx, project.Slug, domain.StatusStopped, domain.StatusStopping, ""); err != nil {
		s.log.Error("orphaned_state: failed to update status after stop",
			"slug", project.Slug, "error", err)
	} else {
		project.PreviousStatus = domain.StatusStopping
		project.Status = domain.StatusStopped
	}
	return nil
}

// --- Reset ---

func (s *projectService) Reset(ctx context.Context, slug string) (*ProjectView, error) {
	project, err := s.projectRepo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, mapNotFoundErr(slug, err)
	}

	// Stop first if running.
	if project.Status == domain.StatusRunning {
		if err := s.stopProject(ctx, project); err != nil {
			return nil, err
		}
	}

	// Validate that we are in a destroyable state.
	if !project.CanDestroy() {
		return nil, &UsecaseError{
			Code:    ErrCodeInvalidState,
			Message: fmt.Sprintf("project %q cannot be reset from status %q", slug, project.Status),
		}
	}

	prev := project.Status
	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusDestroying, prev, ""); err != nil {
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to update status", Err: err}
	}
	project.PreviousStatus = prev
	project.Status = domain.StatusDestroying

	// Destroy runtime resources.
	if err := s.adapter.Destroy(ctx, project); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to destroy runtime resources", Err: err}
	}

	// Re-provision: new secrets + new ports.
	newConfig, provErr := s.provisionConfig(ctx, project, nil)
	if provErr != nil {
		s.setErrorAndLog(ctx, project, provErr.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to provision config", Err: provErr}
	}

	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusCreating, domain.StatusDestroying, ""); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to update status", Err: err}
	}
	project.PreviousStatus = domain.StatusDestroying
	project.Status = domain.StatusCreating

	// Create new runtime resources.
	if err := s.adapter.Create(ctx, project, newConfig); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to create runtime resources", Err: err}
	}

	// Start services.
	if err := s.adapter.Start(ctx, project); err != nil {
		// Best-effort cleanup of the newly created resources.
		if destroyErr := s.adapter.Destroy(ctx, project); destroyErr != nil {
			s.log.Error("orphaned_runtime_resource: failed to clean up after start failure during reset",
				"slug", slug, "start_error", err, "destroy_error", destroyErr)
		}
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to start project after reset", Err: err}
	}

	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusRunning, domain.StatusCreating, ""); err != nil {
		s.log.Error("orphaned_state: failed to update status after reset",
			"slug", slug, "error", err)
	} else {
		project.PreviousStatus = domain.StatusCreating
		project.Status = domain.StatusRunning
	}

	health, _ := s.adapter.Status(ctx, project) // best-effort
	return toProjectView(project, newConfig, health), nil
}

// --- Delete ---

func (s *projectService) Delete(ctx context.Context, slug string) (*ProjectView, error) {
	project, err := s.projectRepo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, mapNotFoundErr(slug, err)
	}
	if !project.CanDestroy() {
		return nil, &UsecaseError{
			Code:    ErrCodeInvalidState,
			Message: fmt.Sprintf("project %q cannot be deleted from status %q", slug, project.Status),
		}
	}

	prev := project.Status
	if err := s.projectRepo.UpdateStatus(ctx, slug, domain.StatusDestroying, prev, ""); err != nil {
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to update status", Err: err}
	}
	project.PreviousStatus = prev
	project.Status = domain.StatusDestroying

	if err := s.adapter.Destroy(ctx, project); err != nil {
		s.setErrorAndLog(ctx, project, err.Error())
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to destroy runtime resources", Err: err}
	}

	if err := s.projectRepo.Delete(ctx, slug); err != nil {
		s.log.Error("orphaned_state: failed to soft-delete project after destroy",
			"slug", slug, "error", err)
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to delete project record", Err: err}
	}

	project.Status = domain.StatusDestroyed
	return toProjectView(project, nil, nil), nil
}

// --- helpers ---

// provisionConfig allocates ports, generates secrets, resolves config, and saves it.
// overrides may be nil for the default case.
func (s *projectService) provisionConfig(ctx context.Context, project *domain.ProjectModel, overrides map[string]string) (*domain.ProjectConfig, error) {
	secrets, err := domain.GenerateProjectSecrets(s.secretGenerator)
	if err != nil {
		return nil, fmt.Errorf("generate secrets: %w", err)
	}

	portSet, err := s.portAllocator.AllocatePorts(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate ports: %w", err)
	}

	config, err := domain.ResolveConfig(project, secrets, portSet, overrides)
	if err != nil {
		return nil, fmt.Errorf("resolve config: %w", err)
	}

	if err := s.configRepo.SaveConfig(ctx, project.Slug, config); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return config, nil
}

// setErrorAndLog sets the project to error state and persists it.
// If UpdateStatus fails, the error is logged but not propagated.
func (s *projectService) setErrorAndLog(ctx context.Context, project *domain.ProjectModel, reason string) {
	prev := project.Status
	_ = project.SetError(reason) // mutates project.Status and LastError
	if err := s.projectRepo.UpdateStatus(ctx, project.Slug, domain.StatusError, prev, reason); err != nil {
		s.log.Error("orphaned_state: failed to persist error status",
			"slug", project.Slug, "reason", reason, "error", err)
	}
}

// --- GetCredentials ---

func (s *projectService) GetCredentials(ctx context.Context, slug string) (*CredentialsView, error) {
	project, err := s.projectRepo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, mapNotFoundErr(slug, err)
	}

	config, err := s.configRepo.GetConfig(ctx, slug)
	if err != nil {
		if errors.Is(err, store.ErrConfigNotFound) {
			return nil, &UsecaseError{
				Code:    ErrCodeNotFound,
				Message: fmt.Sprintf("project %q has no config", slug),
			}
		}
		return nil, &UsecaseError{Code: ErrCodeInternal, Message: "failed to load config", Err: err}
	}

	get := func(key string) string {
		v, _ := config.GetSensitive(key)
		return v
	}

	kongPort := get("KONG_HTTP_PORT")
	publicURL := get("SUPABASE_PUBLIC_URL")

	cv := &CredentialsView{
		Slug:              project.Slug,
		DashboardUsername: get("DASHBOARD_USERNAME"),
		DashboardPassword: get("DASHBOARD_PASSWORD"),
		AnonKey:           get("ANON_KEY"),
		ServiceRoleKey:    get("SERVICE_ROLE_KEY"),
		PostgresHost:      "localhost",
		PostgresPort:      get("POSTGRES_PORT"),
		PostgresDB:        get("POSTGRES_DB"),
		PostgresPassword:  get("POSTGRES_PASSWORD"),
		PoolerPort:        get("POOLER_PROXY_PORT_TRANSACTION"),
	}
	if publicURL != "" {
		cv.APIURL = publicURL
		cv.StudioURL = publicURL
	} else if kongPort != "" {
		base := fmt.Sprintf("http://localhost:%s", kongPort)
		cv.APIURL = base
		cv.StudioURL = base
	}
	return cv, nil
}


func mapNotFoundErr(slug string, err error) *UsecaseError {
	if errors.Is(err, store.ErrProjectNotFound) || errors.Is(err, domain.ErrProjectNotFound) {
		return &UsecaseError{
			Code:    ErrCodeNotFound,
			Message: fmt.Sprintf("project %q not found", slug),
			Err:     err,
		}
	}
	return &UsecaseError{Code: ErrCodeInternal, Message: "failed to load project", Err: err}
}

// toProjectView assembles a ProjectView from a ProjectModel, optional config, and optional health.
func toProjectView(p *domain.ProjectModel, config *domain.ProjectConfig, health *domain.ProjectHealth) *ProjectView {
	v := &ProjectView{
		Slug:           p.Slug,
		DisplayName:    p.DisplayName,
		Status:         string(p.Status),
		PreviousStatus: string(p.PreviousStatus),
		LastError:      p.LastError,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}

	if config != nil {
		masked := make(map[string]string, len(config.Values))
		for k := range config.Values {
			val, _ := config.Get(k) // returns "***" for sensitive keys
			masked[k] = val
		}
		v.Config = masked
		v.URLs = buildURLs(config)
	}

	if health != nil {
		v.Health = toHealthView(health)
	}

	return v
}

// buildURLs constructs ProjectURLs from config values.
// Studio is served through Kong (/* → http://studio:3000), so both API and
// Studio URLs resolve to the same Kong endpoint.
func buildURLs(config *domain.ProjectConfig) *ProjectURLs {
	publicURL, _ := config.Get("SUPABASE_PUBLIC_URL")
	kongPort, _ := config.Get("KONG_HTTP_PORT")

	urls := &ProjectURLs{}
	if publicURL != "" {
		urls.API = publicURL
		urls.Studio = publicURL
	} else if kongPort != "" {
		// Fallback for configs created before SUPABASE_PUBLIC_URL was stored.
		base := fmt.Sprintf("http://localhost:%s", kongPort)
		urls.API = base
		urls.Studio = base
	}
	return urls
}

// toHealthView converts domain.ProjectHealth to HealthView.
func toHealthView(h *domain.ProjectHealth) *HealthView {
	if h == nil {
		return nil
	}
	services := make(map[string]ServiceView, len(h.Services))
	for name, svc := range h.Services {
		services[string(name)] = ServiceView{
			Status:    string(svc.Status),
			Message:   svc.Message,
			CheckedAt: svc.CheckedAt,
		}
	}
	return &HealthView{
		Healthy:   h.IsHealthy(),
		Services:  services,
		CheckedAt: h.CheckedAt,
	}
}
