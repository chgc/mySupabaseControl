package k8s

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// K8sAdapter implements domain.RuntimeAdapter using Helm and kubectl.
type K8sAdapter struct {
	chartRef     string // e.g. "supabase-community/supabase"
	chartVersion string // e.g. "0.5.2"
	dataDir      string // root directory for values.yaml files
	renderer     domain.ConfigRenderer
	runner       cmdRunner
	pollInterval time.Duration // default: 5s
	maxPollTicks int           // default: 24
}

// Static interface assertion.
var _ domain.RuntimeAdapter = (*K8sAdapter)(nil)

// NewK8sAdapter returns a K8sAdapter backed by the OS command runner.
func NewK8sAdapter(chartRef, chartVersion, dataDir string, renderer domain.ConfigRenderer) *K8sAdapter {
	return newK8sAdapterWithRunner(chartRef, chartVersion, dataDir, renderer, &osCmdRunner{})
}

// newK8sAdapterWithRunner is the white-box constructor used in tests.
func newK8sAdapterWithRunner(chartRef, chartVersion, dataDir string, renderer domain.ConfigRenderer, runner cmdRunner) *K8sAdapter {
	return &K8sAdapter{
		chartRef:     chartRef,
		chartVersion: chartVersion,
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
	ns := a.namespace(project.Slug)
	rel := a.releaseName(project.Slug)

	if _, err := a.runner.Run(ctx, "", "helm", "upgrade", "--install", rel, a.chartRef,
		"-n", ns, "--version", a.chartVersion, "-f", a.valuesPath(project.Slug)); err != nil {
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
