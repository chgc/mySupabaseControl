package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

func TestConfigSchema_Completeness(t *testing.T) {
	schema := domain.ConfigSchema()

	t.Run("total count is 94", func(t *testing.T) {
		assert.Len(t, schema, 94)
	})

	t.Run("no duplicate keys", func(t *testing.T) {
		seen := make(map[string]struct{}, len(schema))
		for _, e := range schema {
			_, dup := seen[e.Key]
			assert.False(t, dup, "duplicate key: %s", e.Key)
			seen[e.Key] = struct{}{}
		}
	})

	t.Run("category counts are correct", func(t *testing.T) {
		counts := make(map[domain.ConfigCategory]int)
		for _, e := range schema {
			counts[e.Category]++
		}
		assert.Equal(t, 36, counts[domain.CategoryStaticDefault], "StaticDefault count")
		assert.Equal(t, 16, counts[domain.CategoryPerProject], "PerProject count")
		assert.Equal(t, 12, counts[domain.CategoryGeneratedSecret], "GeneratedSecret count")
		assert.Equal(t, 30, counts[domain.CategoryUserOverridable], "UserOverridable count")
	})

	t.Run("all keys are non-empty strings", func(t *testing.T) {
		for _, e := range schema {
			assert.NotEmpty(t, e.Key, "entry has empty key")
		}
	})

	t.Run("all entries have a valid category", func(t *testing.T) {
		validCategories := map[domain.ConfigCategory]struct{}{
			domain.CategoryStaticDefault:   {},
			domain.CategoryPerProject:      {},
			domain.CategoryGeneratedSecret: {},
			domain.CategoryUserOverridable: {},
		}
		for _, e := range schema {
			_, ok := validCategories[e.Category]
			assert.True(t, ok, "key %q has unknown category %q", e.Key, e.Category)
		}
	})

	t.Run("PerProject entries may have DefaultValue fallbacks", func(t *testing.T) {
		// Some PerProject keys have fallback DefaultValues (e.g. SITE_URL, STORAGE_TENANT_ID).
		// This test just verifies all PerProject entries have a non-empty Key.
		for _, e := range schema {
			if e.Category == domain.CategoryPerProject {
				assert.NotEmpty(t, e.Key, "PerProject entry has empty key")
			}
		}
	})

	t.Run("GeneratedSecret entries have no DefaultValue", func(t *testing.T) {
		for _, e := range schema {
			if e.Category == domain.CategoryGeneratedSecret {
				assert.Empty(t, e.DefaultValue,
					"GeneratedSecret key %q should not have a static DefaultValue", e.Key)
			}
		}
	})

	t.Run("known GeneratedSecret keys are present", func(t *testing.T) {
		wantSecrets := []string{
			"POSTGRES_PASSWORD",
			"JWT_SECRET",
			"ANON_KEY",
			"SERVICE_ROLE_KEY",
			"DASHBOARD_PASSWORD",
		}
		keyIndex := make(map[string]domain.ConfigEntry, len(schema))
		for _, e := range schema {
			keyIndex[e.Key] = e
		}
		for _, k := range wantSecrets {
			e, ok := keyIndex[k]
			require.True(t, ok, "missing GeneratedSecret key: %s", k)
			assert.Equal(t, domain.CategoryGeneratedSecret, e.Category, "key %s should be GeneratedSecret", k)
		}
	})

	t.Run("known PerProject port keys are present", func(t *testing.T) {
		wantPorts := []string{
			"KONG_HTTP_PORT",
			"KONG_HTTPS_PORT",
			"POSTGRES_PORT",
			"STUDIO_PORT",
		}
		keyIndex := make(map[string]domain.ConfigEntry, len(schema))
		for _, e := range schema {
			keyIndex[e.Key] = e
		}
		for _, k := range wantPorts {
			e, ok := keyIndex[k]
			require.True(t, ok, "missing PerProject key: %s", k)
			assert.Equal(t, domain.CategoryPerProject, e.Category, "key %s should be PerProject", k)
		}
	})
}
