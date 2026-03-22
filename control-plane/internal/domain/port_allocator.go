package domain

import "context"

// PortAllocator allocates a conflict-free set of ports for a new project.
// Implementations must be safe for concurrent use (e.g. via a DB-level advisory lock or
// serialised requests) to prevent two simultaneous "create project" calls from receiving
// the same port set.
type PortAllocator interface {
	// AllocatePorts scans existing projects and system-occupied ports, then returns a
	// PortSet with no conflicts. ctx controls DB lock timeouts and request cancellation.
	AllocatePorts(ctx context.Context) (*PortSet, error)
}
