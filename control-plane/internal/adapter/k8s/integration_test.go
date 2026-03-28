//go:build integration

// Integration tests for the K8s adapter against a real Kubernetes cluster.
// These tests require OrbStack K8s (or any k3s/k8s cluster) and Helm + kubectl CLIs.
//
// Prerequisites:
//  1. kubectl must be available and cluster must be reachable.
//  2. helm must be available.
//  3. The supabase-community Helm repo must be added:
//     helm repo add supabase-community https://supabase-community.github.io/helm-charts
//     helm repo update
//
// Run with:
//
//	go test -v -race -tags=integration -timeout=10m ./internal/adapter/k8s/...
package k8s_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/adapter/k8s"
	"github.com/kevin/supabase-control-plane/internal/domain"
)

const (
	intTestSlug     = "k8s-e2e-test"
	intChartRef     = "supabase-community/supabase"
	intChartVersion = "0.5.2"
	intRepoURL      = "https://supabase-community.github.io/helm-charts"
)

// checkPrereqs skips the test if kubectl or helm are not available or if the
// cluster is not reachable.
func checkPrereqs(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not found in PATH — skipping k8s integration test")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not found in PATH — skipping k8s integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "kubectl", "cluster-info").Run(); err != nil {
		t.Skipf("kubectl cluster-info failed (%v) — skipping k8s integration test", err)
	}

	// Verify the Helm repo is available (chart must be searchable).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := exec.CommandContext(ctx2, "helm", "search", "repo", intChartRef, "--version", intChartVersion).Run(); err != nil {
		t.Skipf("helm chart %s@%s not found (run: helm repo add supabase-community https://supabase-community.github.io/helm-charts && helm repo update) — skipping",
			intChartRef, intChartVersion)
	}
}

// testSecrets returns minimal but valid secrets for e2e use.
func testSecrets(t *testing.T) map[string]string {
	t.Helper()
	gen := domain.NewSecretGenerator()
	secrets, err := domain.GenerateProjectSecrets(gen)
	require.NoError(t, err, "GenerateProjectSecrets")
	return secrets
}

// TestIntegration_K8sAdapter_FullLifecycle exercises Create → Start → Status →
// Stop → Destroy against a real Kubernetes cluster.
//
// The test always attempts cleanup in t.Cleanup so the namespace is removed
// even when intermediate steps fail.
func TestIntegration_K8sAdapter_FullLifecycle(t *testing.T) {
	checkPrereqs(t)

	dataDir := t.TempDir()
	renderer := k8s.NewK8sValuesRenderer()
	adapter := k8s.NewK8sAdapter(intChartRef, intChartVersion, intRepoURL, dataDir, renderer)

	project, err := domain.NewProject(intTestSlug, "K8s E2E Test Project", domain.RuntimeKubernetes)
	require.NoError(t, err)

	portSet := &domain.PortSet{
		KongHTTP:     30980,
		PostgresPort: 31432,
		PoolerPort:   0,
	}
	config, err := domain.ResolveConfig(project, testSecrets(t), portSet, nil)
	require.NoError(t, err, "ResolveConfig")

	// Always clean up — best-effort; ignore errors from a partially-created project.
	t.Cleanup(func() {
		destroyProject := &domain.ProjectModel{Slug: intTestSlug, Status: domain.StatusDestroying}
		_ = adapter.Destroy(context.Background(), destroyProject)
	})

	// ── Create ────────────────────────────────────────────────────────────────
	t.Log("Step 1: Create — writing values.yaml and creating namespace")
	require.NoError(t, adapter.Create(context.Background(), project, config), "Create")

	// ── Start ─────────────────────────────────────────────────────────────────
	t.Log("Step 2: Start — helm upgrade --install (this may take a few minutes)")
	startProject := &domain.ProjectModel{Slug: intTestSlug, Status: domain.StatusStarting}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	require.NoError(t, adapter.Start(ctx, startProject), "Start")

	// ── Status ────────────────────────────────────────────────────────────────
	t.Log("Step 3: Status — querying pod health via kubectl")
	runningProject := &domain.ProjectModel{Slug: intTestSlug, Status: domain.StatusRunning}
	health, err := adapter.Status(context.Background(), runningProject)
	require.NoError(t, err, "Status")
	require.NotNil(t, health, "health must not be nil")
	assert.True(t, health.IsHealthy(), "all pods should be healthy after Start returned nil")
	t.Logf("Health: %+v", health)

	// ── Stop ──────────────────────────────────────────────────────────────────
	t.Log("Step 4: Stop — helm uninstall")
	stoppingProject := &domain.ProjectModel{Slug: intTestSlug, Status: domain.StatusStopping}
	require.NoError(t, adapter.Stop(context.Background(), stoppingProject), "Stop")

	// ── Destroy ───────────────────────────────────────────────────────────────
	t.Log("Step 5: Destroy — deleting namespace and local files")
	destroyingProject := &domain.ProjectModel{Slug: intTestSlug, Status: domain.StatusDestroying}
	require.NoError(t, adapter.Destroy(context.Background(), destroyingProject), "Destroy")
}

// TestIntegration_K8sAdapter_CreateIdempotent verifies that running Create
// twice on the same slug is safe (AlreadyExists is tolerated).
func TestIntegration_K8sAdapter_CreateIdempotent(t *testing.T) {
	checkPrereqs(t)

	dataDir := t.TempDir()
	renderer := k8s.NewK8sValuesRenderer()
	adapter := k8s.NewK8sAdapter(intChartRef, intChartVersion, intRepoURL, dataDir, renderer)

	project, err := domain.NewProject("k8s-idem-test", "K8s Idempotent Test", domain.RuntimeKubernetes)
	require.NoError(t, err)

	portSet := &domain.PortSet{KongHTTP: 30981, PostgresPort: 31433, PoolerPort: 0}
	config, err := domain.ResolveConfig(project, testSecrets(t), portSet, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = adapter.Destroy(context.Background(), &domain.ProjectModel{
			Slug:   "k8s-idem-test",
			Status: domain.StatusDestroying,
		})
	})

	require.NoError(t, adapter.Create(context.Background(), project, config), "first Create")
	require.NoError(t, adapter.Create(context.Background(), project, config), "second Create must be idempotent")
}
