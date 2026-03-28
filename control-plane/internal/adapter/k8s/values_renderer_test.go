package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// makeRendererConfig builds a minimal ProjectConfig with valid port values
// so that HelmValuesMapper.MapValues succeeds.
func makeRendererConfig() *domain.ProjectConfig {
	return &domain.ProjectConfig{
		ProjectSlug: "test-project",
		Values: map[string]string{
			"JWT_SECRET":       "test-jwt-secret",
			"ANON_KEY":         "test-anon-key",
			"SERVICE_ROLE_KEY": "test-service-role-key",
			"POSTGRES_PASSWORD": "test-pg-password",
			"KONG_HTTP_PORT":   "8000",
			"POSTGRES_PORT":    "5432",
			"SITE_URL":         "http://localhost:3000",
		},
	}
}

// ── Nil / empty guards ────────────────────────────────────────────────────────

func TestRender_NilConfig(t *testing.T) {
	r := NewK8sValuesRenderer()
	_, err := r.Render(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "config is nil")
}

func TestRender_NilValues(t *testing.T) {
	r := NewK8sValuesRenderer()
	_, err := r.Render(&domain.ProjectConfig{ProjectSlug: "x"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "config.Values is nil")
}

// ── Valid config ──────────────────────────────────────────────────────────────

func TestRender_ValidConfig(t *testing.T) {
	r := NewK8sValuesRenderer()
	artifacts, err := r.Render(makeRendererConfig())
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "values.yaml", artifacts[0].Path)
	assert.Equal(t, uint32(0600), artifacts[0].Mode)
}

// ── YAML structure ───────────────────────────────────────────────────────────

func TestRender_YAMLStructure(t *testing.T) {
	r := NewK8sValuesRenderer()
	artifacts, err := r.Render(makeRendererConfig())
	require.NoError(t, err)

	var parsed map[string]any
	err = yaml.Unmarshal(artifacts[0].Content, &parsed)
	require.NoError(t, err, "output must be valid YAML")

	// Verify nested key paths produced by HelmValuesMapper.
	secret, ok := parsed["secret"].(map[string]any)
	require.True(t, ok, "expected top-level 'secret' map")

	jwt, ok := secret["jwt"].(map[string]any)
	require.True(t, ok, "expected 'secret.jwt' map")
	assert.NotEmpty(t, jwt["secret"], "secret.jwt.secret should be set")
	assert.NotEmpty(t, jwt["anonKey"], "secret.jwt.anonKey should be set")
	assert.NotEmpty(t, jwt["serviceKey"], "secret.jwt.serviceKey should be set")

	db, ok := secret["db"].(map[string]any)
	require.True(t, ok, "expected 'secret.db' map")
	assert.NotEmpty(t, db["password"], "secret.db.password should be set")
}

// ── Sensitive values appear unmasked ──────────────────────────────────────────

func TestRender_SecretValues(t *testing.T) {
	r := NewK8sValuesRenderer()
	cfg := makeRendererConfig()
	cfg.Values["JWT_SECRET"] = "my-real-jwt-secret"

	artifacts, err := r.Render(cfg)
	require.NoError(t, err)

	content := string(artifacts[0].Content)
	assert.Contains(t, content, "my-real-jwt-secret",
		"sensitive value must appear unmasked in values.yaml")
	assert.NotContains(t, content, "***",
		"masked placeholder must not appear in values.yaml")
}

// ── Artifact count ───────────────────────────────────────────────────────────

func TestRender_ArtifactCount(t *testing.T) {
	r := NewK8sValuesRenderer()
	artifacts, err := r.Render(makeRendererConfig())
	require.NoError(t, err)
	assert.Len(t, artifacts, 1, "must return exactly 1 artifact")
}

// ── Transform error propagation ──────────────────────────────────────────────

func TestRender_TransformError(t *testing.T) {
	r := NewK8sValuesRenderer()
	cfg := makeRendererConfig()
	cfg.Values["KONG_HTTP_PORT"] = "not-a-number"

	_, err := r.Render(cfg)
	require.Error(t, err, "invalid port string should cause an error")
	assert.ErrorContains(t, err, "KONG_HTTP_PORT")
}
