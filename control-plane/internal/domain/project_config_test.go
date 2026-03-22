package domain_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// allTestSecrets returns all 12 GeneratedSecret keys with dummy values,
// satisfying ResolveConfig's Required check.
func allTestSecrets() map[string]string {
	return map[string]string{
		"JWT_SECRET":                    "test-jwt-secret",
		"ANON_KEY":                      "test-anon",
		"SERVICE_ROLE_KEY":              "test-service",
		"POSTGRES_PASSWORD":             "test-pg-pw",
		"DASHBOARD_PASSWORD":            "test-dash",
		"SECRET_KEY_BASE":               "test-secret-key-base",
		"VAULT_ENC_KEY":                 "test-vault-enc",
		"PG_META_CRYPTO_KEY":            "test-pg-meta-crypto",
		"LOGFLARE_PUBLIC_ACCESS_TOKEN":  "test-logflare-pub",
		"LOGFLARE_PRIVATE_ACCESS_TOKEN": "test-logflare-priv",
		"S3_PROTOCOL_ACCESS_KEY_ID":     "test-s3-key-id",
		"S3_PROTOCOL_ACCESS_KEY_SECRET": "test-s3-key-secret",
	}
}

// helper builds a minimal PortSet for use in tests.
func testPortSet() *domain.PortSet {
	return &domain.PortSet{
		KongHTTP:     8000,
		PostgresPort: 5432,
		PoolerPort:   6543,
		StudioPort:   3000,
		MetaPort:     8080,
		ImgProxyPort: 5001,
	}
}

// helper builds a minimal project for use in tests.
func testProject(t *testing.T) *domain.ProjectModel {
	t.Helper()
	p, err := domain.NewProject("test-slug", "Test Project")
	require.NoError(t, err)
	return p
}

// ─── ResolveConfig — priority order ─────────────────────────────────────────

func TestResolveConfig_StaticDefaultFallback(t *testing.T) {
	p := testProject(t)
	cfg, err := domain.ResolveConfig(p, allTestSecrets(), testPortSet(), nil)
	require.NoError(t, err)

	// POSTGRES_DB is StaticDefault — should be resolved.
	val, ok := cfg.GetSensitive("POSTGRES_DB")
	assert.True(t, ok)
	assert.NotEmpty(t, val)
}

func TestResolveConfig_PerProjectOverridesStaticDefault(t *testing.T) {
	p := testProject(t)
	ps := testPortSet()
	cfg, err := domain.ResolveConfig(p, allTestSecrets(), ps, nil)
	require.NoError(t, err)

	// KONG_HTTP_PORT is PerProject and should equal ps.KongHTTP.
	val, ok := cfg.GetSensitive("KONG_HTTP_PORT")
	require.True(t, ok)
	assert.Equal(t, "8000", val)

	// KONG_HTTPS_PORT is derived as KongHTTP+1.
	httpsVal, ok := cfg.GetSensitive("KONG_HTTPS_PORT")
	require.True(t, ok)
	assert.Equal(t, "8001", httpsVal)
}

func TestResolveConfig_GeneratedSecretUsed(t *testing.T) {
	p := testProject(t)
	secrets := allTestSecrets()
	cfg, err := domain.ResolveConfig(p, secrets, testPortSet(), nil)
	require.NoError(t, err)

	val, ok := cfg.GetSensitive("JWT_SECRET")
	require.True(t, ok)
	assert.Equal(t, "test-jwt-secret", val)

	// Get() on a secret key should return masked value.
	masked, ok := cfg.Get("JWT_SECRET")
	require.True(t, ok)
	assert.Equal(t, "***", masked)
}

func TestResolveConfig_UserOverrideWins(t *testing.T) {
	p := testProject(t)
	overrides := map[string]string{
		"DISABLE_SIGNUP": "true", // UserOverridable key
	}
	cfg, err := domain.ResolveConfig(p, allTestSecrets(), testPortSet(), overrides)
	require.NoError(t, err)

	val, ok := cfg.GetSensitive("DISABLE_SIGNUP")
	require.True(t, ok)
	assert.Equal(t, "true", val)
	assert.Equal(t, overrides, cfg.Overrides)
}

func TestResolveConfig_NilSecrets_ReturnsErrMissingRequired(t *testing.T) {
	// All GeneratedSecret keys are Required. Nil secrets should trigger ErrMissingRequiredConfig.
	p := testProject(t)
	_, err := domain.ResolveConfig(p, nil, testPortSet(), nil)
	require.Error(t, err)

	var missingErr *domain.ErrMissingRequiredConfig
	assert.ErrorAs(t, err, &missingErr)
	assert.NotEmpty(t, missingErr.Keys)
}

// ─── ResolveConfig — error cases ─────────────────────────────────────────────

func TestResolveConfig_ErrConfigNotOverridable(t *testing.T) {
	p := testProject(t)
	overrides := map[string]string{
		"JWT_SECRET": "should-not-allow", // GeneratedSecret, not UserOverridable
	}
	_, err := domain.ResolveConfig(p, nil, testPortSet(), overrides)
	require.Error(t, err)

	var notOverridable *domain.ErrConfigNotOverridable
	require.ErrorAs(t, err, &notOverridable)
	assert.Equal(t, "JWT_SECRET", notOverridable.Key)
}

func TestResolveConfig_ErrConfigNotOverridable_UnknownKey(t *testing.T) {
	p := testProject(t)
	overrides := map[string]string{
		"TOTALLY_UNKNOWN_KEY": "value",
	}
	_, err := domain.ResolveConfig(p, nil, testPortSet(), overrides)
	require.Error(t, err)

	var notOverridable *domain.ErrConfigNotOverridable
	assert.ErrorAs(t, err, &notOverridable)
}

func TestResolveConfig_NilPortSet_ReturnsError(t *testing.T) {
	p := testProject(t)
	_, err := domain.ResolveConfig(p, nil, nil, nil)
	require.Error(t, err)

	var portErr *domain.ErrInvalidPortSet
	assert.ErrorAs(t, err, &portErr)
}

// ─── ExtractPortSet ───────────────────────────────────────────────────────────

func TestExtractPortSet_RoundTrip(t *testing.T) {
	p := testProject(t)
	original := testPortSet()
	cfg, err := domain.ResolveConfig(p, allTestSecrets(), original, nil)
	require.NoError(t, err)

	extracted, err := domain.ExtractPortSet(cfg)
	require.NoError(t, err)

	assert.Equal(t, original.KongHTTP, extracted.KongHTTP)
	assert.Equal(t, original.PostgresPort, extracted.PostgresPort)
	assert.Equal(t, original.PoolerPort, extracted.PoolerPort)
	assert.Equal(t, original.StudioPort, extracted.StudioPort)
	assert.Equal(t, original.MetaPort, extracted.MetaPort)
	assert.Equal(t, original.ImgProxyPort, extracted.ImgProxyPort)
}

func TestExtractPortSet_ImgProxyBind_ColonFormat(t *testing.T) {
	// IMGPROXY_BIND is stored as ":{port}" — ExtractPortSet must strip the colon.
	cfg := &domain.ProjectConfig{
		ProjectSlug: "test",
		Values: map[string]string{
			"KONG_HTTP_PORT":             "8000",
			"POSTGRES_PORT":              "5432",
			"POOLER_PROXY_PORT_TRANSACTION": "6543",
			"STUDIO_PORT":                "3000",
			"PG_META_PORT":               "8080",
			"IMGPROXY_BIND":              ":5001",
		},
	}
	ps, err := domain.ExtractPortSet(cfg)
	require.NoError(t, err)
	assert.Equal(t, 5001, ps.ImgProxyPort)
}

func TestExtractPortSet_MissingKey_ReturnsError(t *testing.T) {
	cfg := &domain.ProjectConfig{
		ProjectSlug: "test",
		Values: map[string]string{
			// KONG_HTTP_PORT missing intentionally
			"POSTGRES_PORT":              "5432",
			"POOLER_PROXY_PORT_TRANSACTION": "6543",
			"STUDIO_PORT":                "3000",
			"PG_META_PORT":               "8080",
			"IMGPROXY_BIND":              ":5001",
		},
	}
	_, err := domain.ExtractPortSet(cfg)
	require.Error(t, err)

	var portErr *domain.ErrInvalidPortSet
	require.ErrorAs(t, err, &portErr)
	assert.Equal(t, "KONG_HTTP_PORT", portErr.Key)
}

func TestExtractPortSet_InvalidValue_ReturnsError(t *testing.T) {
	cfg := &domain.ProjectConfig{
		ProjectSlug: "test",
		Values: map[string]string{
			"KONG_HTTP_PORT":             "not-a-number",
			"POSTGRES_PORT":              "5432",
			"POOLER_PROXY_PORT_TRANSACTION": "6543",
			"STUDIO_PORT":                "3000",
			"PG_META_PORT":               "8080",
			"IMGPROXY_BIND":              ":5001",
		},
	}
	_, err := domain.ExtractPortSet(cfg)
	require.Error(t, err)

	var portErr *domain.ErrInvalidPortSet
	require.ErrorAs(t, err, &portErr)
	assert.Equal(t, "KONG_HTTP_PORT", portErr.Key)
	assert.Equal(t, "not-a-number", portErr.Value)
}

// ─── Get / GetSensitive ───────────────────────────────────────────────────────

func TestProjectConfig_GetMasksSensitiveKeys(t *testing.T) {
	cfg := &domain.ProjectConfig{
		ProjectSlug: "test",
		Values: map[string]string{
			"JWT_SECRET": "super-secret",
			"POSTGRES_DB": "postgres",
		},
		Overrides: map[string]string{},
	}

	// Sensitive key via Get → masked.
	masked, ok := cfg.Get("JWT_SECRET")
	assert.True(t, ok)
	assert.Equal(t, "***", masked)

	// Non-sensitive key → real value.
	val, ok := cfg.Get("POSTGRES_DB")
	assert.True(t, ok)
	assert.Equal(t, "postgres", val)

	// GetSensitive always returns raw.
	raw, ok := cfg.GetSensitive("JWT_SECRET")
	assert.True(t, ok)
	assert.Equal(t, "super-secret", raw)
}

func TestProjectConfig_Get_MissingKey(t *testing.T) {
	cfg := &domain.ProjectConfig{
		Values:    map[string]string{},
		Overrides: map[string]string{},
	}
	_, ok := cfg.Get("NONEXISTENT")
	assert.False(t, ok)
}

// ─── ExtractSecrets ───────────────────────────────────────────────────────────

func TestExtractSecrets_RoundTrip(t *testing.T) {
	p := testProject(t)
	input := allTestSecrets()
	cfg, err := domain.ResolveConfig(p, input, testPortSet(), nil)
	require.NoError(t, err)

	extracted := domain.ExtractSecrets(cfg)
	for k, v := range input {
		assert.Equal(t, v, extracted[k], "mismatch for key %s", k)
	}
}

// ─── ErrConfigNotOverridable ─────────────────────────────────────────────────

func TestErrConfigNotOverridable_ErrorInterface(t *testing.T) {
	err := &domain.ErrConfigNotOverridable{Key: "JWT_SECRET"}
	assert.Contains(t, err.Error(), "JWT_SECRET")

	var target *domain.ErrConfigNotOverridable
	assert.True(t, errors.As(err, &target))
}
