package domain

import "context"

// MockRuntimeAdapter is a test double for RuntimeAdapter.
// Each method can be overridden by assigning a function to the corresponding field.
// If a field is nil the method is a no-op (returns nil error / zero value).
type MockRuntimeAdapter struct {
	CreateFn      func(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
	StartFn       func(ctx context.Context, project *ProjectModel) error
	StopFn        func(ctx context.Context, project *ProjectModel) error
	DestroyFn     func(ctx context.Context, project *ProjectModel) error
	StatusFn      func(ctx context.Context, project *ProjectModel) (*ProjectHealth, error)
	RenderConfigFn func(ctx context.Context, project *ProjectModel, config *ProjectConfig) ([]Artifact, error)
	ApplyConfigFn  func(ctx context.Context, project *ProjectModel, config *ProjectConfig) error
}

// Create delegates to CreateFn if set, otherwise returns nil.
func (m *MockRuntimeAdapter) Create(ctx context.Context, project *ProjectModel, config *ProjectConfig) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, project, config)
	}
	return nil
}

// Start delegates to StartFn if set, otherwise returns nil.
func (m *MockRuntimeAdapter) Start(ctx context.Context, project *ProjectModel) error {
	if m.StartFn != nil {
		return m.StartFn(ctx, project)
	}
	return nil
}

// Stop delegates to StopFn if set, otherwise returns nil.
func (m *MockRuntimeAdapter) Stop(ctx context.Context, project *ProjectModel) error {
	if m.StopFn != nil {
		return m.StopFn(ctx, project)
	}
	return nil
}

// Destroy delegates to DestroyFn if set, otherwise returns nil.
func (m *MockRuntimeAdapter) Destroy(ctx context.Context, project *ProjectModel) error {
	if m.DestroyFn != nil {
		return m.DestroyFn(ctx, project)
	}
	return nil
}

// Status delegates to StatusFn if set, otherwise returns an empty ProjectHealth.
func (m *MockRuntimeAdapter) Status(ctx context.Context, project *ProjectModel) (*ProjectHealth, error) {
	if m.StatusFn != nil {
		return m.StatusFn(ctx, project)
	}
	return &ProjectHealth{Services: map[ServiceName]ServiceHealth{}}, nil
}

// RenderConfig delegates to RenderConfigFn if set, otherwise returns nil.
func (m *MockRuntimeAdapter) RenderConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) ([]Artifact, error) {
	if m.RenderConfigFn != nil {
		return m.RenderConfigFn(ctx, project, config)
	}
	return nil, nil
}

// ApplyConfig delegates to ApplyConfigFn if set, otherwise returns nil.
func (m *MockRuntimeAdapter) ApplyConfig(ctx context.Context, project *ProjectModel, config *ProjectConfig) error {
	if m.ApplyConfigFn != nil {
		return m.ApplyConfigFn(ctx, project, config)
	}
	return nil
}
