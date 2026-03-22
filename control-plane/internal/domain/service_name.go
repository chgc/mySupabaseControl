// Package domain contains all core types, interfaces, and business logic
// for the Supabase Control Plane.
package domain

// ServiceName identifies a service within a Supabase project stack.
type ServiceName string

const (
	ServiceDB         ServiceName = "db"
	ServiceAuth       ServiceName = "auth"
	ServiceRest       ServiceName = "rest"
	ServiceRealtime   ServiceName = "realtime"
	ServiceStorage    ServiceName = "storage"
	ServiceImgProxy   ServiceName = "imgproxy"
	ServiceKong       ServiceName = "kong"
	ServiceMeta       ServiceName = "meta"
	ServiceFunctions  ServiceName = "functions"
	ServiceAnalytics  ServiceName = "analytics"
	ServiceSupavisor  ServiceName = "supavisor"
	ServiceStudio     ServiceName = "studio"
	ServiceVector     ServiceName = "vector"
)
