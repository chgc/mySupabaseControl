package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

func newTestProject(slug string, status domain.ProjectStatus) *domain.ProjectModel {
	return &domain.ProjectModel{Slug: slug, Status: status}
}

func newTestConfig() *domain.ProjectConfig {
	return &domain.ProjectConfig{
		ProjectSlug: "myproject",
		Values:      map[string]string{"KEY": "val"},
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_WritesFilesToDisk(t *testing.T) {
	dir := t.TempDir()
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, &fakeCmdRunner{})
	project := newTestProject("myproject", domain.StatusCreating)

	err := adapter.Create(context.Background(), project, newTestConfig())
	require.NoError(t, err)

	projectDir := filepath.Join(dir, "myproject")
	_, err = os.Stat(projectDir)
	require.NoError(t, err, "project directory should exist")

	envContent, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=val\n", string(envContent))

	composeContent, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	require.NoError(t, err)
	assert.Equal(t, composeTemplate, composeContent)
}

func TestCreate_RendererError_DirectoryNotCreated(t *testing.T) {
	dir := t.TempDir()
	renderErr := errors.New("render failed")
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{
		RenderFn: func(_ *domain.ProjectConfig) ([]domain.Artifact, error) {
			return nil, renderErr
		},
	}, &fakeCmdRunner{})

	err := adapter.Create(context.Background(), newTestProject("myproject", domain.StatusCreating), newTestConfig())
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "create", adapterErr.Operation)

	// Directory should NOT exist — renderer failed before MkdirAll.
	_, statErr := os.Stat(filepath.Join(dir, "myproject"))
	assert.True(t, os.IsNotExist(statErr), "project directory should not be created when renderer fails")
}

func TestCreate_NoCmdRunnerCalls(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.Create(context.Background(), newTestProject("myproject", domain.StatusCreating), newTestConfig())
	require.NoError(t, err)
	assert.Empty(t, runner.Calls, "Create must not invoke any docker compose commands")
}

// ── Stop ──────────────────────────────────────────────────────────────────────

func TestStop_RunsComposeStop(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.Stop(context.Background(), newTestProject("myproject", domain.StatusStopping))
	require.NoError(t, err)

	require.Len(t, runner.Calls, 1)
	call := runner.Calls[0]
	assert.Equal(t, filepath.Join(dir, "myproject"), call.Dir)
	assert.Equal(t, "docker", call.Name)
	assert.Equal(t, []string{"compose", "stop"}, call.Args)
}

func TestStop_RunnerError_ReturnsAdapterError(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("docker not found")
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.Stop(context.Background(), newTestProject("myproject", domain.StatusStopping))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "stop", adapterErr.Operation)
}

// ── Destroy ───────────────────────────────────────────────────────────────────

func TestDestroy_RemovesContainersAndDirectory(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0700))

	runner := &fakeCmdRunner{}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newTestProject("myproject", domain.StatusDestroying))
	require.NoError(t, err)

	require.Len(t, runner.Calls, 1)
	call := runner.Calls[0]
	assert.Equal(t, []string{"compose", "down", "-v", "--remove-orphans"}, call.Args)

	_, statErr := os.Stat(projectDir)
	assert.True(t, os.IsNotExist(statErr), "project directory should be removed")
}

func TestDestroy_DownFailure_StillAttemptsCleanup(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0700))

	downErr := errors.New("compose down failed")
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, downErr
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newTestProject("myproject", domain.StatusDestroying))
	require.Error(t, err)

	// Should return the downErr wrapped in AdapterError.
	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "destroy", adapterErr.Operation)
	assert.ErrorIs(t, adapterErr.Err, downErr)

	// Directory should still have been removed (best-effort cleanup).
	_, statErr := os.Stat(projectDir)
	assert.True(t, os.IsNotExist(statErr), "directory should still be removed even if down failed")
}

// ── Status ────────────────────────────────────────────────────────────────────

func TestStatus_ParsesHealthyOutput(t *testing.T) {
	dir := t.TempDir()
	ndjson := `{"Service":"db","State":"running","Health":"healthy"}` + "\n" +
		`{"Service":"kong","State":"running","Health":"healthy"}`

	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte(ndjson), nil
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	health, err := adapter.Status(context.Background(), newTestProject("myproject", domain.StatusRunning))
	require.NoError(t, err)
	assert.True(t, health.IsHealthy())
}

func TestStatus_EmptyOutput_ReturnsEmptyHealth(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte(""), nil
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	health, err := adapter.Status(context.Background(), newTestProject("myproject", domain.StatusStopped))
	require.NoError(t, err)
	assert.Empty(t, health.Services)
}

func TestStatus_RunnerError_ReturnsAdapterError(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("exec error")
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	_, err := adapter.Status(context.Background(), newTestProject("myproject", domain.StatusRunning))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "status", adapterErr.Operation)
}

// ── RenderConfig ──────────────────────────────────────────────────────────────

func TestRenderConfig_DelegatesToRenderer_NoDiskWrite(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{}
	expectedArtifacts := []domain.Artifact{{Path: ".env", Content: []byte("X=1\n"), Mode: 0600}}

	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{
		RenderFn: func(_ *domain.ProjectConfig) ([]domain.Artifact, error) {
			return expectedArtifacts, nil
		},
	}, runner)

	artifacts, err := adapter.RenderConfig(context.Background(), newTestProject("myproject", domain.StatusRunning), newTestConfig())
	require.NoError(t, err)
	assert.Equal(t, expectedArtifacts, artifacts)
	assert.Empty(t, runner.Calls, "RenderConfig must not invoke docker compose")

	// Nothing written to disk.
	entries, _ := os.ReadDir(filepath.Join(dir, "myproject"))
	assert.Empty(t, entries)
}

// ── ApplyConfig ───────────────────────────────────────────────────────────────

func TestApplyConfig_WritesEnvAndReconciles_WhenRunning(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0700))

	runner := &fakeCmdRunner{}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.ApplyConfig(context.Background(), newTestProject("myproject", domain.StatusRunning), newTestConfig())
	require.NoError(t, err)

	// .env should be written.
	_, err = os.Stat(filepath.Join(projectDir, ".env"))
	require.NoError(t, err)

	// Should have called `docker compose up -d`.
	require.Len(t, runner.Calls, 1)
	assert.Equal(t, []string{"compose", "up", "-d"}, runner.Calls[0].Args)
}

func TestApplyConfig_WritesOnly_WhenStopped(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0700))

	runner := &fakeCmdRunner{}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.ApplyConfig(context.Background(), newTestProject("myproject", domain.StatusStopped), newTestConfig())
	require.NoError(t, err)

	// .env should be written.
	_, err = os.Stat(filepath.Join(projectDir, ".env"))
	require.NoError(t, err)

	// Must NOT call docker compose when stopped.
	assert.Empty(t, runner.Calls)
}

// ── Start (basic) ─────────────────────────────────────────────────────────────
// Full health-polling is tested only lightly here to avoid long sleep loops.
// The ticker-based polling is verified via context cancellation.

func TestStart_UpFailure_ReturnsStartError(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("docker daemon unavailable")
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	err := adapter.Start(context.Background(), newTestProject("myproject", domain.StatusStarting))
	require.Error(t, err)

	var startErr *domain.StartError
	require.ErrorAs(t, err, &startErr)
	assert.Equal(t, "myproject", startErr.Slug)
}

func TestStart_ContextCanceled_ReturnsCtxErr(t *testing.T) {
	dir := t.TempDir()
	callCount := 0
	runner := &fakeCmdRunner{
		RunFn: func(ctx context.Context, _, _ string, args ...string) ([]byte, error) {
			callCount++
			if callCount == 1 {
				// First call is `up -d` — succeed.
				return nil, nil
			}
			// Status calls return unhealthy.
			return []byte(`{"Service":"db","State":"running","Health":"starting"}`), nil
		},
	}
	adapter := newComposeAdapterWithRunner(dir, &mockConfigRenderer{}, runner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately after up -d succeeds

	// Start will succeed at `up -d`, then the ticker select will pick ctx.Done().
	err := adapter.Start(ctx, newTestProject("myproject", domain.StatusStarting))
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
