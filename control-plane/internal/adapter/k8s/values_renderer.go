package k8s

import (
	"fmt"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"gopkg.in/yaml.v3"
)

// K8sValuesRenderer implements domain.ConfigRenderer by producing a Helm values.yaml.
type K8sValuesRenderer struct {
	mapper *HelmValuesMapper
}

// Static interface assertion.
var _ domain.ConfigRenderer = (*K8sValuesRenderer)(nil)

// NewK8sValuesRenderer returns a K8sValuesRenderer wired to the default HelmValuesMapper.
func NewK8sValuesRenderer() *K8sValuesRenderer {
	return &K8sValuesRenderer{mapper: NewHelmValuesMapper()}
}

// Render converts config into a values.yaml Artifact suitable for Helm chart consumption.
//
// Returns an error if:
//   - config is nil
//   - config.Values is nil
//   - HelmValuesMapper.MapValues fails (e.g. invalid port string)
//   - yaml.Marshal fails
func (r *K8sValuesRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error) {
	if config == nil {
		return nil, fmt.Errorf("k8s renderer: config is nil")
	}
	if config.Values == nil {
		return nil, fmt.Errorf("k8s renderer: config.Values is nil")
	}

	vals, err := r.mapper.MapValues(config)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := yaml.Marshal(vals)
	if err != nil {
		return nil, fmt.Errorf("k8s renderer: yaml marshal: %w", err)
	}

	return []domain.Artifact{{
		Path:    "values.yaml",
		Content: yamlBytes,
		Mode:    0600,
	}}, nil
}
