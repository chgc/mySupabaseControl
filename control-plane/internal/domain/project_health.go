package domain

import "time"

// ServiceStatus represents the health state of a single service.
type ServiceStatus string

const (
	ServiceStatusHealthy   ServiceStatus = "healthy"
	ServiceStatusUnhealthy ServiceStatus = "unhealthy"
	ServiceStatusStarting  ServiceStatus = "starting"
	ServiceStatusStopped   ServiceStatus = "stopped"
	ServiceStatusUnknown   ServiceStatus = "unknown"
)

// ServiceHealth holds the current health information for a single service.
type ServiceHealth struct {
	Status    ServiceStatus
	// Message provides a human-readable description, e.g. the container exit reason.
	Message   string
	CheckedAt time.Time
}

// ProjectHealth aggregates the health of all services in a project.
type ProjectHealth struct {
	// Services maps each ServiceName to its current health info.
	Services  map[ServiceName]ServiceHealth
	CheckedAt time.Time
}

// IsHealthy returns true only when every service reports a healthy status.
// Returns false if h is nil or Services is empty.
func (h *ProjectHealth) IsHealthy() bool {
	if h == nil || len(h.Services) == 0 {
		return false
	}
	for _, s := range h.Services {
		if s.Status != ServiceStatusHealthy {
			return false
		}
	}
	return true
}

// AllServices returns all Supabase service names in docker-compose startup order:
// db → auth → rest → realtime → storage → imgproxy → meta → functions →
// kong → studio → analytics → vector → supavisor
func AllServices() []ServiceName {
	return []ServiceName{
		ServiceDB, ServiceAuth, ServiceRest, ServiceRealtime,
		ServiceStorage, ServiceImgProxy, ServiceMeta, ServiceFunctions,
		ServiceKong, ServiceStudio, ServiceAnalytics, ServiceVector, ServiceSupavisor,
	}
}
