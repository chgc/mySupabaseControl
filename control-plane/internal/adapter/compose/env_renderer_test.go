package compose

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

func makeConfig(values map[string]string) *domain.ProjectConfig {
	return &domain.ProjectConfig{
		ProjectSlug: "test-project",
		Values:      values,
	}
}

// ── Nil / empty guards ────────────────────────────────────────────────────────

func TestRender_NilConfig_ReturnsError(t *testing.T) {
	r := NewComposeEnvRenderer()
	_, err := r.Render(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "config is nil")
}

func TestRender_NilValues_ReturnsError(t *testing.T) {
	r := NewComposeEnvRenderer()
	_, err := r.Render(&domain.ProjectConfig{ProjectSlug: "x"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "config.Values is nil")
}

func TestRender_EmptyValues_ProducesEmptyContent(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{}))
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, ".env", artifacts[0].Path)
	assert.Empty(t, artifacts[0].Content)
	assert.Equal(t, uint32(0600), artifacts[0].Mode)
}

// ── Simple key=value (no quoting needed) ─────────────────────────────────────

func TestRender_SimpleValues_NoQuotes(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{
		"FOO": "bar",
		"BAZ": "123",
	}))
	require.NoError(t, err)
	content := string(artifacts[0].Content)
	assert.Contains(t, content, "BAZ=123\n")
	assert.Contains(t, content, "FOO=bar\n")
}

// ── Keys are sorted ──────────────────────────────────────────────────────────

func TestRender_KeysAreSorted(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{
		"ZEBRA": "z",
		"ALPHA": "a",
		"MANGO": "m",
	}))
	require.NoError(t, err)
	content := string(artifacts[0].Content)
	alphaIdx := len(content) - len(content[indexOf(content, "ALPHA"):])
	mangoIdx := len(content) - len(content[indexOf(content, "MANGO"):])
	zebraIdx := len(content) - len(content[indexOf(content, "ZEBRA"):])
	assert.Less(t, alphaIdx, mangoIdx)
	assert.Less(t, mangoIdx, zebraIdx)
}

func indexOf(s, sub string) int {
	for i := range s {
		if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ── Quoting and escaping ──────────────────────────────────────────────────────

func TestRender_ValueWithHash_IsQuoted(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"KEY": "foo#bar"}))
	require.NoError(t, err)
	assert.Contains(t, string(artifacts[0].Content), `KEY="foo#bar"`)
}

func TestRender_ValueWithSpace_IsQuoted(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"KEY": "hello world"}))
	require.NoError(t, err)
	assert.Contains(t, string(artifacts[0].Content), `KEY="hello world"`)
}

func TestRender_ValueWithDollar_IsEscaped(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"KEY": "$SECRET"}))
	require.NoError(t, err)
	// $ must be escaped to \$ inside double-quoted strings (godotenv interpolation prevention).
	assert.Contains(t, string(artifacts[0].Content), `KEY="\$SECRET"`)
}

func TestRender_ValueWithBackslash_IsEscaped(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"KEY": `path\to\file`}))
	require.NoError(t, err)
	assert.Contains(t, string(artifacts[0].Content), `KEY="path\\to\\file"`)
}

func TestRender_ValueWithDoubleQuote_IsEscaped(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"KEY": `say "hello"`}))
	require.NoError(t, err)
	assert.Contains(t, string(artifacts[0].Content), `KEY="say \"hello\""`)
}

func TestRender_ValueWithBackslashAndDollar_EscapeOrderCorrect(t *testing.T) {
	// Backslash must be escaped first; if dollar were escaped first,
	// the backslash escape would double-escape it.
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"KEY": `\$VAR`}))
	require.NoError(t, err)
	// \$VAR → escape \→\\ first → \\$VAR → escape $→\$ → \\\$VAR
	assert.Contains(t, string(artifacts[0].Content), `KEY="\\\$VAR"`)
}

// ── Control character rejection ───────────────────────────────────────────────

func TestRender_ValueWithNewline_ReturnsError(t *testing.T) {
	r := NewComposeEnvRenderer()
	_, err := r.Render(makeConfig(map[string]string{"KEY": "line1\nline2"}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "control character")
	assert.ErrorContains(t, err, `"KEY"`)
}

func TestRender_ValueWithCarriageReturn_ReturnsError(t *testing.T) {
	r := NewComposeEnvRenderer()
	_, err := r.Render(makeConfig(map[string]string{"KEY": "value\r"}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "control character")
}

// ── GetSensitive is used (not Get) ────────────────────────────────────────────

func TestRender_SensitiveValue_UsesRealValue(t *testing.T) {
	// Build a config where a sensitive key has a real value in Values.
	// Verify that Render outputs the real value, not "***".
	r := NewComposeEnvRenderer()

	// Find a sensitive key from the schema.
	var sensitiveKey string
	for _, e := range domain.ConfigSchema() {
		if e.Sensitive {
			sensitiveKey = e.Key
			break
		}
	}
	require.NotEmpty(t, sensitiveKey, "no sensitive key found in schema")

	cfg := &domain.ProjectConfig{
		ProjectSlug: "proj",
		Values:      map[string]string{sensitiveKey: "super-secret"},
	}

	artifacts, err := r.Render(cfg)
	require.NoError(t, err)
	content := string(artifacts[0].Content)
	assert.Contains(t, content, "super-secret")
	assert.NotContains(t, content, "***")
}

// ── Artifact metadata ─────────────────────────────────────────────────────────

func TestRender_ArtifactPath_And_Mode(t *testing.T) {
	r := NewComposeEnvRenderer()
	artifacts, err := r.Render(makeConfig(map[string]string{"K": "v"}))
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, ".env", artifacts[0].Path)
	assert.Equal(t, uint32(0600), artifacts[0].Mode)
}
