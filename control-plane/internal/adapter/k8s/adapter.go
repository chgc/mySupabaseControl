package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// K8sAdapter implements domain.RuntimeAdapter using Helm and kubectl.
type K8sAdapter struct {
	chartRef     string // e.g. "supabase-community/supabase"
	chartVersion string // e.g. "0.5.2"
	repoURL      string // e.g. "https://supabase-community.github.io/helm-charts"
	dataDir      string // root directory for values.yaml files
	renderer     domain.ConfigRenderer
	runner       cmdRunner
	pollInterval time.Duration // default: 5s
	maxPollTicks int           // default: 24
}

// Static interface assertion.
var _ domain.RuntimeAdapter = (*K8sAdapter)(nil)

// NewK8sAdapter returns a K8sAdapter backed by the OS command runner.
func NewK8sAdapter(chartRef, chartVersion, repoURL, dataDir string, renderer domain.ConfigRenderer) *K8sAdapter {
	return newK8sAdapterWithRunner(chartRef, chartVersion, repoURL, dataDir, renderer, &osCmdRunner{})
}

// newK8sAdapterWithRunner is the white-box constructor used in tests.
func newK8sAdapterWithRunner(chartRef, chartVersion, repoURL, dataDir string, renderer domain.ConfigRenderer, runner cmdRunner) *K8sAdapter {
	return &K8sAdapter{
		chartRef:     chartRef,
		chartVersion: chartVersion,
		repoURL:      repoURL,
		dataDir:      dataDir,
		renderer:     renderer,
		runner:       runner,
		pollInterval: 5 * time.Second,
		maxPollTicks: 24,
	}
}

func (a *K8sAdapter) namespace(slug string) string   { return "supabase-" + slug }
func (a *K8sAdapter) releaseName(slug string) string { return "supabase-" + slug }
func (a *K8sAdapter) valuesDir(slug string) string   { return filepath.Join(a.dataDir, slug) }
func (a *K8sAdapter) valuesPath(slug string) string {
	return filepath.Join(a.valuesDir(slug), "values.yaml")
}

// Create renders config artifacts, writes them to the values directory,
// and creates the Kubernetes namespace.
func (a *K8sAdapter) Create(ctx context.Context, project *domain.ProjectModel, config *domain.ProjectConfig) error {
	artifacts, err := a.renderer.Render(config)
	if err != nil {
		return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
	}

	if err := os.MkdirAll(a.valuesDir(project.Slug), 0700); err != nil {
		return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
	}

	for _, art := range artifacts {
		dst := filepath.Join(a.valuesDir(project.Slug), art.Path)
		if err := os.WriteFile(dst, art.Content, os.FileMode(art.Mode)); err != nil {
			return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
		}
	}

	ns := a.namespace(project.Slug)
	out, err := a.runner.Run(ctx, "", "kubectl", "create", "namespace", ns)
	if err != nil {
		if !strings.Contains(string(out), "AlreadyExists") {
			return &domain.AdapterError{Operation: "create", Slug: project.Slug, Err: err}
		}
	}

	return nil
}

// Start installs or upgrades the Helm release and polls until all pods are
// healthy or the timeout is reached.
func (a *K8sAdapter) Start(ctx context.Context, project *domain.ProjectModel) error {
	if err := a.ensureHelmRepo(ctx); err != nil {
		return &domain.StartError{Slug: project.Slug, Err: err}
	}

	ns := a.namespace(project.Slug)
	rel := a.releaseName(project.Slug)

	if _, err := a.runner.Run(ctx, "", "helm", "upgrade", "--install", rel, a.chartRef,
		"-n", ns, "--version", a.chartVersion, "-f", a.valuesPath(project.Slug)); err != nil {
		return &domain.StartError{Slug: project.Slug, Err: err}
	}

	if err := a.patchNodePorts(ctx, project.Slug, ns); err != nil {
		return &domain.StartError{Slug: project.Slug, Err: err}
	}

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	var lastHealth *domain.ProjectHealth
	ticks := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ticks++
			h, err := a.Status(ctx, project)
			if err == nil && h.IsHealthy() {
				return nil
			}
			lastHealth = h
			if ticks >= a.maxPollTicks {
				return &domain.StartError{
					Slug:   project.Slug,
					Err:    domain.ErrServiceNotHealthy,
					Health: lastHealth,
				}
			}
		}
	}
}

// Stop uninstalls the Helm release. Ignores "not found" errors for idempotency.
func (a *K8sAdapter) Stop(ctx context.Context, project *domain.ProjectModel) error {
	ns := a.namespace(project.Slug)
	rel := a.releaseName(project.Slug)

	out, err := a.runner.Run(ctx, "", "helm", "uninstall", rel, "-n", ns)
	if err != nil {
		if !strings.Contains(string(out), "not found") {
			return &domain.AdapterError{Operation: "stop", Slug: project.Slug, Err: err}
		}
	}
	return nil
}

// Destroy performs best-effort cleanup: Helm uninstall, namespace deletion,
// and local values directory removal. Returns the first error encountered.
func (a *K8sAdapter) Destroy(ctx context.Context, project *domain.ProjectModel) error {
	ns := a.namespace(project.Slug)
	rel := a.releaseName(project.Slug)

	// Step 1: Helm uninstall
	out, helmErr := a.runner.Run(ctx, "", "helm", "uninstall", rel, "-n", ns)
	if helmErr != nil && strings.Contains(string(out), "not found") {
		helmErr = nil
	}

	// Step 2: Delete namespace
	out, nsErr := a.runner.Run(ctx, "", "kubectl", "delete", "namespace", ns)
	if nsErr != nil && strings.Contains(string(out), "not found") {
		nsErr = nil
	}

	// Step 3: Remove local values directory
	cleanupErr := os.RemoveAll(a.valuesDir(project.Slug))

	// Return first non-nil error
	if helmErr != nil {
		return &domain.AdapterError{Operation: "destroy", Slug: project.Slug, Err: helmErr}
	}
	if nsErr != nil {
		return &domain.AdapterError{Operation: "destroy:cleanup", Slug: project.Slug, Err: nsErr}
	}
	if cleanupErr != nil {
		return &domain.AdapterError{Operation: "destroy:cleanup", Slug: project.Slug, Err: cleanupErr}
	}
	return nil
}

// Status queries pod health via kubectl and parses the result.
func (a *K8sAdapter) Status(ctx context.Context, project *domain.ProjectModel) (*domain.ProjectHealth, error) {
	ns := a.namespace(project.Slug)

	out, err := a.runner.Run(ctx, "", "kubectl", "get", "pods", "-n", ns, "-o", "json")
	if err != nil {
		return nil, &domain.AdapterError{Operation: "status", Slug: project.Slug, Err: err}
	}

	return parseK8sPods(out), nil
}

// RenderConfig delegates to the renderer without any side effects.
func (a *K8sAdapter) RenderConfig(_ context.Context, _ *domain.ProjectModel, config *domain.ProjectConfig) ([]domain.Artifact, error) {
	return a.renderer.Render(config)
}

// ApplyConfig renders and writes configuration. If the project is running,
// also performs a helm upgrade to reconcile.
func (a *K8sAdapter) ApplyConfig(ctx context.Context, project *domain.ProjectModel, config *domain.ProjectConfig) error {
	artifacts, err := a.renderer.Render(config)
	if err != nil {
		return &domain.AdapterError{Operation: "apply-config", Slug: project.Slug, Err: err}
	}

	for _, art := range artifacts {
		dst := filepath.Join(a.valuesDir(project.Slug), art.Path)
		if err := os.WriteFile(dst, art.Content, os.FileMode(art.Mode)); err != nil {
			return &domain.AdapterError{Operation: "apply-config", Slug: project.Slug, Err: err}
		}
	}

	if project.Status == domain.StatusRunning {
		ns := a.namespace(project.Slug)
		rel := a.releaseName(project.Slug)
		if _, err := a.runner.Run(ctx, "", "helm", "upgrade", rel, a.chartRef,
			"-n", ns, "--version", a.chartVersion, "-f", a.valuesPath(project.Slug)); err != nil {
			return &domain.AdapterError{Operation: "apply-config:reconcile", Slug: project.Slug, Err: err}
		}
	}

	return nil
}

// patchNodePorts reads the project values.yaml and patches the Kong and DB
// services to use the configured nodePort values. The Helm chart does not
// expose nodePort in its values schema, so Kubernetes would otherwise assign
// random ports — making the allocated ports unreachable.
func (a *K8sAdapter) patchNodePorts(ctx context.Context, slug, ns string) error {
	raw, err := os.ReadFile(a.valuesPath(slug))
	if err != nil {
		return fmt.Errorf("read values: %w", err)
	}

	var vals map[string]any
	if err := yaml.Unmarshal(raw, &vals); err != nil {
		return fmt.Errorf("parse values: %w", err)
	}

	type svcPatch struct {
		svcName  string
		valueKey string // dot path into vals
	}

	patches := []svcPatch{
		{svcName: "supabase-" + slug + "-supabase-kong", valueKey: "service.kong.nodePort"},
		{svcName: "supabase-" + slug + "-supabase-db", valueKey: "service.db.nodePort"},
	}

	for _, p := range patches {
		port := getNestedInt(vals, p.valueKey)
		if port == 0 {
			continue
		}
		patch := fmt.Sprintf(`[{"op":"replace","path":"/spec/ports/0/nodePort","value":%d}]`, port)
		if _, err := a.runner.Run(ctx, "", "kubectl", "patch", "svc", p.svcName,
			"-n", ns, "--type=json", "-p", patch); err != nil {
			return fmt.Errorf("patch nodePort for %s: %w", p.svcName, err)
		}
	}

	return nil
}

// ensureHelmRepo checks whether the Helm repo for chartRef is already configured.// If the repoURL is empty the check is skipped (useful in tests or custom setups).
// If the repo is missing it is added and the index is updated.
func (a *K8sAdapter) ensureHelmRepo(ctx context.Context) error {
	if a.repoURL == "" {
		return nil
	}

	// Derive repo name from the chart reference (e.g. "supabase-community" from
	// "supabase-community/supabase").
	repoName := a.chartRef
	if idx := strings.Index(a.chartRef, "/"); idx != -1 {
		repoName = a.chartRef[:idx]
	}

	// `helm repo list` exits 1 with "no repositories configured" when the list is
	// empty, so we treat any non-zero exit as "repo not present" only when the
	// output does not contain the repo name as a column value (not just a URL substring).
	out, _ := a.runner.Run(ctx, "", "helm", "repo", "list")
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 && fields[0] == repoName {
			return nil // already configured by name
		}
	}

	// Add the repo.
	if _, err := a.runner.Run(ctx, "", "helm", "repo", "add", repoName, a.repoURL); err != nil {
		return fmt.Errorf("helm repo add %s %s: %w", repoName, a.repoURL, err)
	}

	// Update the index so the pinned chart version is available.
	if _, err := a.runner.Run(ctx, "", "helm", "repo", "update", repoName); err != nil {
		return fmt.Errorf("helm repo update %s: %w", repoName, err)
	}

	return nil
}

