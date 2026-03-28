package k8s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

func newAdapterTestProject(slug string, status domain.ProjectStatus) *domain.ProjectModel {
	return &domain.ProjectModel{Slug: slug, Status: status}
}

func newAdapterTestConfig() *domain.ProjectConfig {
	return &domain.ProjectConfig{
		ProjectSlug: "myproject",
		Values:      map[string]string{"KEY": "val"},
	}
}

// ── Mock types ────────────────────────────────────────────────────────────────

type fakeCall struct {
	Dir  string
	Name string
	Args []string
}

type fakeCmdRunner struct {
	RunFn func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	Calls []fakeCall
}

func (f *fakeCmdRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	f.Calls = append(f.Calls, fakeCall{Dir: dir, Name: name, Args: args})
	if f.RunFn != nil {
		return f.RunFn(ctx, dir, name, args...)
	}
	return nil, nil
}

type mockRenderer struct {
	RenderFn func(config *domain.ProjectConfig) ([]domain.Artifact, error)
}

func (m *mockRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error) {
	if m.RenderFn != nil {
		return m.RenderFn(config)
	}
	return []domain.Artifact{{Path: "values.yaml", Content: []byte("key: val\n"), Mode: 0600}}, nil
}

func newTestAdapter(dataDir string, renderer domain.ConfigRenderer, runner *fakeCmdRunner) *K8sAdapter {
	a := newK8sAdapterWithRunner("supabase-community/supabase", "0.5.2", dataDir, renderer, runner)
	a.pollInterval = 1 * time.Millisecond
	a.maxPollTicks = 3
	return a
}

// healthyPodsJSON returns kubectl JSON output with healthy kong and db pods.
func healthyPodsJSON() []byte {
	return []byte(`{
  "items": [
    {
      "metadata": {"name": "supabase-myproject-kong-abc", "labels": {"app.kubernetes.io/name": "supabase-kong"}},
      "status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
    },
    {
      "metadata": {"name": "supabase-myproject-db-def", "labels": {"app.kubernetes.io/name": "supabase-db"}},
      "status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
    }
  ]
}`)
}

// unhealthyPodsJSON returns kubectl JSON output with a pending pod.
func unhealthyPodsJSON() []byte {
	return []byte(`{
  "items": [
    {
      "metadata": {"name": "supabase-myproject-db-abc", "labels": {"app.kubernetes.io/name": "supabase-db"}},
      "status": {"phase": "Pending", "conditions": []}
    }
  ]
}`)
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_Success(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Create(context.Background(), newAdapterTestProject("myproject", domain.StatusCreating), newAdapterTestConfig())
	require.NoError(t, err)

	// Verify values.yaml was written.
	content, err := os.ReadFile(filepath.Join(dir, "myproject", "values.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "key: val\n", string(content))

	// Verify kubectl create namespace was called.
	require.Len(t, runner.Calls, 1)
	call := runner.Calls[0]
	assert.Equal(t, "", call.Dir)
	assert.Equal(t, "kubectl", call.Name)
	assert.Equal(t, []string{"create", "namespace", "supabase-myproject"}, call.Args)
}

func TestCreate_RenderError(t *testing.T) {
	dir := t.TempDir()
	renderErr := errors.New("render failed")
	adapter := newTestAdapter(dir, &mockRenderer{
		RenderFn: func(_ *domain.ProjectConfig) ([]domain.Artifact, error) {
			return nil, renderErr
		},
	}, &fakeCmdRunner{})

	err := adapter.Create(context.Background(), newAdapterTestProject("myproject", domain.StatusCreating), newAdapterTestConfig())
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "create", adapterErr.Operation)

	// Directory should NOT exist.
	_, statErr := os.Stat(filepath.Join(dir, "myproject"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestCreate_NamespaceError(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte("error: something went wrong"), fmt.Errorf("exit status 1")
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Create(context.Background(), newAdapterTestProject("myproject", domain.StatusCreating), newAdapterTestConfig())
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "create", adapterErr.Operation)
}

func TestCreate_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte(`Error from server (AlreadyExists): namespaces "supabase-myproject" already exists`),
				fmt.Errorf("exit status 1")
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Create(context.Background(), newAdapterTestProject("myproject", domain.StatusCreating), newAdapterTestConfig())
	require.NoError(t, err)

	// values.yaml should still be written.
	_, statErr := os.Stat(filepath.Join(dir, "myproject", "values.yaml"))
	require.NoError(t, statErr)
}

// ── Start ─────────────────────────────────────────────────────────────────────

func TestStart_Success(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "myproject"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "myproject", "values.yaml"), []byte("x: 1"), 0600))

	callCount := 0
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			callCount++
			if callCount == 1 {
				// helm upgrade --install
				return nil, nil
			}
			// kubectl get pods (Status call) — return healthy
			return healthyPodsJSON(), nil
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Start(context.Background(), newAdapterTestProject("myproject", domain.StatusStarting))
	require.NoError(t, err)

	// First call: helm upgrade --install, subsequent: kubectl get pods
	require.GreaterOrEqual(t, len(runner.Calls), 2)
	assert.Equal(t, "helm", runner.Calls[0].Name)
	assert.Equal(t, "kubectl", runner.Calls[1].Name)
}

func TestStart_HelmError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "myproject"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "myproject", "values.yaml"), []byte("x: 1"), 0600))

	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("helm not found")
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Start(context.Background(), newAdapterTestProject("myproject", domain.StatusStarting))
	require.Error(t, err)

	var startErr *domain.StartError
	require.ErrorAs(t, err, &startErr)
	assert.Equal(t, "myproject", startErr.Slug)
}

func TestStart_Timeout(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "myproject"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "myproject", "values.yaml"), []byte("x: 1"), 0600))

	callCount := 0
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			callCount++
			if callCount == 1 {
				return nil, nil // helm succeeds
			}
			return unhealthyPodsJSON(), nil // status always unhealthy
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Start(context.Background(), newAdapterTestProject("myproject", domain.StatusStarting))
	require.Error(t, err)

	var startErr *domain.StartError
	require.ErrorAs(t, err, &startErr)
	assert.ErrorIs(t, startErr.Err, domain.ErrServiceNotHealthy)
	assert.NotNil(t, startErr.Health)
}

func TestStart_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "myproject"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "myproject", "values.yaml"), []byte("x: 1"), 0600))

	callCount := 0
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			callCount++
			if callCount == 1 {
				cancel() // cancel context after helm succeeds
				return nil, nil
			}
			return unhealthyPodsJSON(), nil
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Start(ctx, newAdapterTestProject("myproject", domain.StatusStarting))
	require.ErrorIs(t, err, context.Canceled)
}

// ── Stop ──────────────────────────────────────────────────────────────────────

func TestStop_Success(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Stop(context.Background(), newAdapterTestProject("myproject", domain.StatusStopping))
	require.NoError(t, err)

	require.Len(t, runner.Calls, 1)
	call := runner.Calls[0]
	assert.Equal(t, "", call.Dir)
	assert.Equal(t, "helm", call.Name)
	assert.Equal(t, []string{"uninstall", "supabase-myproject", "-n", "supabase-myproject"}, call.Args)
}

func TestStop_Error(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte("Error: something failed"), errors.New("exit status 1")
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Stop(context.Background(), newAdapterTestProject("myproject", domain.StatusStopping))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "stop", adapterErr.Operation)
}

func TestStop_NotFound(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte(`Error: uninstall: Release not found: supabase-myproject`),
				errors.New("exit status 1")
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Stop(context.Background(), newAdapterTestProject("myproject", domain.StatusStopping))
	require.NoError(t, err)
}

// ── Destroy ───────────────────────────────────────────────────────────────────

func TestDestroy_Success(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(valuesDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(valuesDir, "values.yaml"), []byte("x: 1"), 0600))

	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newAdapterTestProject("myproject", domain.StatusDestroying))
	require.NoError(t, err)

	// Verify both commands were called.
	require.Len(t, runner.Calls, 2)
	assert.Equal(t, "helm", runner.Calls[0].Name)
	assert.Equal(t, []string{"uninstall", "supabase-myproject", "-n", "supabase-myproject"}, runner.Calls[0].Args)
	assert.Equal(t, "kubectl", runner.Calls[1].Name)
	assert.Equal(t, []string{"delete", "namespace", "supabase-myproject"}, runner.Calls[1].Args)

	// Values directory should be removed.
	_, statErr := os.Stat(valuesDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestDestroy_BestEffort(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(valuesDir, 0700))

	helmErr := errors.New("helm failed")
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, name string, _ ...string) ([]byte, error) {
			if name == "helm" {
				return []byte("Error: helm failed"), helmErr
			}
			return nil, nil
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newAdapterTestProject("myproject", domain.StatusDestroying))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "destroy", adapterErr.Operation)

	// ALL 3 steps should have been attempted.
	require.Len(t, runner.Calls, 2, "both helm and kubectl should be called even if helm fails")

	// Values directory should still be removed (best-effort).
	_, statErr := os.Stat(valuesDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestDestroy_Step2OnlyFails(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(valuesDir, 0700))

	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, name string, args ...string) ([]byte, error) {
			if name == "kubectl" {
				return []byte("Error: forbidden"), errors.New("exit status 1")
			}
			return nil, nil
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newAdapterTestProject("myproject", domain.StatusDestroying))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "destroy:cleanup", adapterErr.Operation)
}

func TestDestroy_NamespaceAlreadyDeleted(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(valuesDir, 0700))

	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, name string, _ ...string) ([]byte, error) {
			if name == "kubectl" {
				return []byte(`Error from server (NotFound): namespaces "supabase-myproject" not found`),
					errors.New("exit status 1")
			}
			return nil, nil
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newAdapterTestProject("myproject", domain.StatusDestroying))
	require.NoError(t, err)
}

func TestDestroy_Step3OnlyFails(t *testing.T) {
	// Use a dataDir with null byte to force os.RemoveAll to fail.
	dir := "/nonexistent\x00invalid"
	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.Destroy(context.Background(), newAdapterTestProject("myproject", domain.StatusDestroying))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "destroy:cleanup", adapterErr.Operation)
}

// ── Status ────────────────────────────────────────────────────────────────────

func TestStatus_Success(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return healthyPodsJSON(), nil
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	health, err := adapter.Status(context.Background(), newAdapterTestProject("myproject", domain.StatusRunning))
	require.NoError(t, err)
	require.NotNil(t, health)

	assert.Contains(t, health.Services, domain.ServiceKong)
	assert.Contains(t, health.Services, domain.ServiceDB)
	assert.Equal(t, domain.ServiceStatusHealthy, health.Services[domain.ServiceKong].Status)

	// Verify kubectl args.
	require.Len(t, runner.Calls, 1)
	assert.Equal(t, "kubectl", runner.Calls[0].Name)
	assert.Equal(t, []string{"get", "pods", "-n", "supabase-myproject", "-o", "json"}, runner.Calls[0].Args)
}

func TestStatus_Error(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{
		RunFn: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("kubectl not found")
		},
	}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	_, err := adapter.Status(context.Background(), newAdapterTestProject("myproject", domain.StatusRunning))
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "status", adapterErr.Operation)
}

// ── RenderConfig ──────────────────────────────────────────────────────────────

func TestRenderConfig_Delegates(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeCmdRunner{}
	expected := []domain.Artifact{{Path: "values.yaml", Content: []byte("custom: true\n"), Mode: 0600}}
	adapter := newTestAdapter(dir, &mockRenderer{
		RenderFn: func(_ *domain.ProjectConfig) ([]domain.Artifact, error) {
			return expected, nil
		},
	}, runner)

	artifacts, err := adapter.RenderConfig(context.Background(), newAdapterTestProject("myproject", domain.StatusRunning), newAdapterTestConfig())
	require.NoError(t, err)
	assert.Equal(t, expected, artifacts)
	assert.Empty(t, runner.Calls, "RenderConfig must not invoke any commands")
}

// ── ApplyConfig ───────────────────────────────────────────────────────────────

func TestApplyConfig_Running(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(valuesDir, 0700))

	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.ApplyConfig(context.Background(), newAdapterTestProject("myproject", domain.StatusRunning), newAdapterTestConfig())
	require.NoError(t, err)

	// values.yaml should be written.
	_, statErr := os.Stat(filepath.Join(valuesDir, "values.yaml"))
	require.NoError(t, statErr)

	// Should have called helm upgrade for reconcile.
	require.Len(t, runner.Calls, 1)
	assert.Equal(t, "helm", runner.Calls[0].Name)
	assert.Contains(t, runner.Calls[0].Args, "upgrade")
}

func TestApplyConfig_Stopped(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "myproject")
	require.NoError(t, os.MkdirAll(valuesDir, 0700))

	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.ApplyConfig(context.Background(), newAdapterTestProject("myproject", domain.StatusStopped), newAdapterTestConfig())
	require.NoError(t, err)

	// values.yaml should be written.
	_, statErr := os.Stat(filepath.Join(valuesDir, "values.yaml"))
	require.NoError(t, statErr)

	// Must NOT call helm when stopped.
	assert.Empty(t, runner.Calls)
}

func TestApplyConfig_RenderError(t *testing.T) {
	dir := t.TempDir()
	adapter := newTestAdapter(dir, &mockRenderer{
		RenderFn: func(_ *domain.ProjectConfig) ([]domain.Artifact, error) {
			return nil, errors.New("render boom")
		},
	}, &fakeCmdRunner{})

	err := adapter.ApplyConfig(context.Background(), newAdapterTestProject("myproject", domain.StatusRunning), newAdapterTestConfig())
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "apply-config", adapterErr.Operation)
}

func TestApplyConfig_WriteError(t *testing.T) {
	// Use a non-existent parent directory so WriteFile fails.
	dir := filepath.Join(t.TempDir(), "nonexistent", "deep")
	runner := &fakeCmdRunner{}
	adapter := newTestAdapter(dir, &mockRenderer{}, runner)

	err := adapter.ApplyConfig(context.Background(), newAdapterTestProject("myproject", domain.StatusRunning), newAdapterTestConfig())
	require.Error(t, err)

	var adapterErr *domain.AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, "apply-config", adapterErr.Operation)
}
