package usecase

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// ProjectView is the read model returned by ProjectService operations.
// Sensitive config values are masked ("***") via domain.ProjectConfig.Get.
type ProjectView struct {
	Slug           string            `json:"slug"`
	DisplayName    string            `json:"display_name"`
	Status         string            `json:"status"`
	PreviousStatus string            `json:"previous_status,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Health         *HealthView       `json:"health,omitempty"`
	// Config contains masked config values (sensitive fields show "***").
	// Only populated by Get; omitted by List for performance.
	Config map[string]string `json:"config,omitempty"`
	// URLs provides convenient access to project endpoints.
	URLs *ProjectURLs `json:"urls,omitempty"`
}

// HealthView is the serialisable representation of domain.ProjectHealth.
type HealthView struct {
	Healthy   bool                   `json:"healthy"`
	Services  map[string]ServiceView `json:"services"`
	CheckedAt time.Time              `json:"checked_at"`
}

// ServiceView is the serialisable representation of domain.ServiceHealth.
type ServiceView struct {
	// Status is one of: healthy, unhealthy, starting, stopped, unknown.
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// ProjectURLs contains the public-facing URLs for a running project.
type ProjectURLs struct {
	API      string `json:"api"`      // Kong API gateway URL
	Studio   string `json:"studio"`   // Supabase Studio URL
	Inbucket string `json:"inbucket"` // Email testing URL (dev only)
}

// CredentialsView contains all information needed to access a project's admin UI
// and APIs. Sensitive values (passwords, keys) are returned in plain text.
// Only expose this to trusted operators.
type CredentialsView struct {
	Slug    string `json:"slug"`
	// Admin UI
	StudioURL         string `json:"studio_url"`
	DashboardUsername string `json:"dashboard_username"`
	DashboardPassword string `json:"dashboard_password"`
	// API access
	APIURL         string `json:"api_url"`
	AnonKey        string `json:"anon_key"`
	ServiceRoleKey string `json:"service_role_key"`
	// Direct DB access
	PostgresHost     string `json:"postgres_host"`
	PostgresPort     string `json:"postgres_port"`
	PostgresDB       string `json:"postgres_db"`
	PostgresPassword string `json:"postgres_password"`
	// Supavisor (connection pooler)
	PoolerPort string `json:"pooler_port"`
}

// UsecaseError is returned by all ProjectService methods.
// It wraps lower-level errors (store, adapter, domain) with operation context.
type UsecaseError struct {
	// Code is a stable machine-readable error code.
	Code ErrorCode
	// Message is a human-readable description safe to surface to end users.
	Message string
	// Err is the underlying cause (may be nil for pure domain errors).
	Err error
}

// Error implements the error interface.
func (e *UsecaseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap allows errors.Is / errors.As to inspect the underlying error.
func (e *UsecaseError) Unwrap() error { return e.Err }

// ErrorCode classifies UsecaseError for programmatic handling.
type ErrorCode string

// Stable error codes returned by ProjectService.
const (
	ErrCodeNotFound     ErrorCode = "not_found"
	ErrCodeConflict     ErrorCode = "conflict"
	ErrCodeInvalidInput ErrorCode = "invalid_input"
	ErrCodeInvalidState ErrorCode = "invalid_state"
	ErrCodeInternal     ErrorCode = "internal"
)

// Config holds the external dependencies required to construct a ProjectService.
type Config struct {
	ProjectRepo     store.ProjectRepository
	ConfigRepo      store.ConfigRepository
	Adapter         domain.RuntimeAdapter
	PortAllocator   domain.PortAllocator
	SecretGenerator domain.SecretGenerator
	// Logger is optional; falls back to slog.Default() when nil.
	Logger *slog.Logger
}
