package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RuntimeAdapter defines the abstraction between the Control Plane and the
// underlying execution runtime. Both the Docker Compose Adapter (Phase 2) and
// the K8s Adapter (Phase 5) implement this interface.
type RuntimeAdapter interface {
	// Create establishes the isolation boundary and persistent storage for a project.
	// Docker Compose: creates project directory, renders .env file.
	// K8s: creates namespace and PVCs.
	// Precondition: project.Status == creating
	// Postcondition: success → stopped; failure → error
	Create(ctx context.Context, project *ProjectModel, config *ProjectConfig) error

	// Start deploys and starts all project services.
	// Docker Compose: docker compose up -d
	// K8s: helm upgrade --install
	// Precondition: project.Status == stopped || starting
	// Postcondition: success → running; failure → error (returns *StartError)
	Start(ctx context.Context, project *ProjectModel) error

	// Stop halts all project services while preserving data.
	// Docker Compose: docker compose stop
	// K8s: helm uninstall (releases all pod resources; namespace and PVCs preserved)
	// Precondition: project.Status == running || stopping
	// Postcondition: success → stopped; failure → error
	Stop(ctx context.Context, project *ProjectModel) error

	// Destroy removes all project resources including persistent data.
	// Docker Compose: docker compose down -v + delete project directory
	// K8s: delete namespace (cascades to all resources)
	// Precondition: project.Status == stopped || destroying || error
	// Postcondition: success → destroyed; failure → error
	Destroy(ctx context.Context, project *ProjectModel) error

	// Status queries the health of all services in the project.
	// Does not mutate project.Status — returns a point-in-time snapshot.
	Status(ctx context.Context, project *ProjectModel) (*ProjectHealth, error)

	// RenderConfig renders project configuration into runtime-specific artifacts
	// for inspection. Pure computation — does not write to the runtime.
	// Use ApplyConfig to both render and write.
	RenderConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) ([]Artifact, error)

	// ApplyConfig renders and writes configuration to the runtime.
	// Idempotent: safe to call repeatedly with the same inputs.
	// Docker Compose: overwrites .env file.
	// K8s: kubectl apply ConfigMap/Secret.
	ApplyConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
}

// AdapterError is returned when a RuntimeAdapter operation fails.
type AdapterError struct {
	// Operation is the name of the failing method ("create", "start", "stop",
	// "destroy", "status", "render_config", "apply_config").
	Operation string
	// Slug is the project identifier.
	Slug string
	// Err is the underlying cause.
	Err error
}

func (e *AdapterError) Error() string {
	return fmt.Sprintf("adapter %s failed for project %q: %v", e.Operation, e.Slug, e.Err)
}

// Unwrap allows errors.Is / errors.As to inspect the underlying error.
func (e *AdapterError) Unwrap() error {
	return e.Err
}

// StartError is returned by Start when one or more services fail their health
// checks. It carries a health snapshot so the caller does not need to call
// Status separately.
type StartError struct {
	// Slug is the project identifier.
	Slug string
	// Health is the service health snapshot captured at the time of failure.
	Health *ProjectHealth
	// Err is the underlying cause (typically ErrServiceNotHealthy or ErrAdapterTimeout).
	Err error
}

func (e *StartError) Error() string {
	return fmt.Sprintf("start failed for project %q: %v", e.Slug, e.Err)
}

// Unwrap allows errors.Is / errors.As to inspect the underlying error.
func (e *StartError) Unwrap() error {
	return e.Err
}

// Sentinel errors for RuntimeAdapter operations.
var (
	ErrAdapterTimeout      = errors.New("adapter operation timed out")
	ErrServiceNotHealthy   = errors.New("one or more services failed health check")
	ErrRuntimeNotFound     = errors.New("runtime not available")
	ErrInvalidRuntimeType  = errors.New("invalid runtime type")
)

// RuntimeType identifies the supported runtime backends.
type RuntimeType string

// Supported runtime backend types.
const (
	RuntimeDockerCompose RuntimeType = "docker-compose"
	RuntimeKubernetes    RuntimeType = "kubernetes"
)

// ValidateRuntimeType checks that the given RuntimeType is a known value.
func ValidateRuntimeType(rt RuntimeType) error {
	switch rt {
	case RuntimeDockerCompose, RuntimeKubernetes:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrInvalidRuntimeType, rt)
	}
}

// adapterOptions holds the configuration for constructing a RuntimeAdapter.
type adapterOptions struct {
	composeFilePath string
	projectsDir     string
	timeout         time.Duration
}

// AdapterOption is a functional option for NewRuntimeAdapter.
type AdapterOption func(*adapterOptions)

// WithComposeFilePath sets the path to the docker-compose.yml file.
func WithComposeFilePath(path string) AdapterOption {
	return func(o *adapterOptions) { o.composeFilePath = path }
}

// WithProjectsDir sets the root directory where project subdirectories are created.
func WithProjectsDir(dir string) AdapterOption {
	return func(o *adapterOptions) { o.projectsDir = dir }
}

// WithTimeout sets the default operation timeout.
func WithTimeout(timeout time.Duration) AdapterOption {
	return func(o *adapterOptions) { o.timeout = timeout }
}

// NewRuntimeAdapter constructs a RuntimeAdapter for the given RuntimeType.
// Phase 1 returns a stub implementation for all runtime types.
// Phase 2 will provide a real Docker Compose implementation.
// Phase 5 will provide a real Kubernetes implementation.
func NewRuntimeAdapter(rt RuntimeType, opts ...AdapterOption) (RuntimeAdapter, error) {
	o := &adapterOptions{}
	for _, opt := range opts {
		opt(o)
	}
	switch rt {
	case RuntimeDockerCompose, RuntimeKubernetes:
		return &stubRuntimeAdapter{rt: rt}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrRuntimeNotFound, rt)
	}
}

// stubRuntimeAdapter is the Phase 1 placeholder that satisfies the interface
// without performing any real operations.
type stubRuntimeAdapter struct {
	rt RuntimeType
}

func (s *stubRuntimeAdapter) Create(_ context.Context, _ *ProjectModel, _ *ProjectConfig) error {
	return nil
}

func (s *stubRuntimeAdapter) Start(_ context.Context, _ *ProjectModel) error {
	return nil
}

func (s *stubRuntimeAdapter) Stop(_ context.Context, _ *ProjectModel) error {
	return nil
}

func (s *stubRuntimeAdapter) Destroy(_ context.Context, _ *ProjectModel) error {
	return nil
}

func (s *stubRuntimeAdapter) Status(_ context.Context, _ *ProjectModel) (*ProjectHealth, error) {
	return &ProjectHealth{
		Services:  map[ServiceName]ServiceHealth{},
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (s *stubRuntimeAdapter) RenderConfig(_ context.Context, _ *ProjectModel, _ *ProjectConfig) ([]Artifact, error) {
	return nil, nil
}

func (s *stubRuntimeAdapter) ApplyConfig(_ context.Context, _ *ProjectModel, _ *ProjectConfig) error {
	return nil
}

// GlobalHealth holds the health status of globally shared services.
type GlobalHealth struct {
	Services  map[ServiceName]ServiceHealth
	CheckedAt time.Time
}

// GlobalServiceManager manages globally shared services (e.g. vector log collector).
// This interface is optional; implementations may choose not to support it.
type GlobalServiceManager interface {
	// EnsureGlobalServices ensures globally shared services are running.
	// Idempotent: no-op if they are already running.
	EnsureGlobalServices(ctx context.Context) error

	// GlobalStatus returns a health snapshot of global services.
	GlobalStatus(ctx context.Context) (*GlobalHealth, error)
}
