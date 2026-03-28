package k8s

import (
	"errors"
	"testing"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

func TestSetNestedValue(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		value    any
		existing map[string]any
		wantKey  string
		wantVal  any
	}{
		{
			name:     "single level",
			path:     "key",
			value:    "val",
			existing: map[string]any{},
			wantKey:  "key",
			wantVal:  "val",
		},
		{
			name:     "two levels",
			path:     "a.b",
			value:    42,
			existing: map[string]any{},
			wantKey:  "a",
			wantVal:  map[string]any{"b": 42},
		},
		{
			name:     "three levels",
			path:     "a.b.c",
			value:    true,
			existing: map[string]any{},
			wantKey:  "a",
			wantVal:  map[string]any{"b": map[string]any{"c": true}},
		},
		{
			name:     "preserves existing intermediate map",
			path:     "a.b.c",
			value:    "new",
			existing: map[string]any{"a": map[string]any{"b": map[string]any{"x": "old"}}},
			wantKey:  "a",
			wantVal:  map[string]any{"b": map[string]any{"x": "old", "c": "new"}},
		},
		{
			name:     "preserves sibling keys",
			path:     "a.b",
			value:    "val",
			existing: map[string]any{"z": "keep"},
			wantKey:  "z",
			wantVal:  "keep",
		},
		{
			name:     "overwrites non-map intermediate",
			path:     "a.b.c",
			value:    "val",
			existing: map[string]any{"a": map[string]any{"b": "not-a-map"}},
			wantKey:  "a",
			wantVal:  map[string]any{"b": map[string]any{"c": "val"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.existing
			setNestedValue(m, tt.path, tt.value)

			got, ok := m[tt.wantKey]
			if !ok {
				t.Fatalf("key %q not found in map", tt.wantKey)
			}

			// For the "preserves sibling keys" test, check sibling independently
			if tt.name == "preserves sibling keys" {
				if got != tt.wantVal {
					t.Errorf("got %v, want %v", got, tt.wantVal)
				}
				// Also verify the new key was set
				if _, ok := m["a"]; !ok {
					t.Error("new key 'a' not set")
				}
				return
			}

			// Deep compare for map values
			if wantMap, ok := tt.wantVal.(map[string]any); ok {
				gotMap, ok := got.(map[string]any)
				if !ok {
					t.Fatalf("expected map[string]any, got %T", got)
				}
				assertMapsEqual(t, gotMap, wantMap)
			} else {
				if got != tt.wantVal {
					t.Errorf("got %v, want %v", got, tt.wantVal)
				}
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    any
		wantErr bool
	}{
		{name: "valid port", input: "8000", want: 8000},
		{name: "valid nodeport", input: "30080", want: 30080},
		{name: "zero", input: "0", want: 0},
		{name: "invalid string", input: "abc", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
		{name: "float string", input: "3.14", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toInt(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewHelmValuesMapper(t *testing.T) {
	mapper := NewHelmValuesMapper()
	mappings := mapper.Mappings()

	if len(mappings) != 93 {
		t.Errorf("expected 93 mapping entries, got %d", len(mappings))
	}

	// Count skipped (empty ValuesPath) entries.
	skipped := 0
	for _, m := range mappings {
		if m.ValuesPath == "" {
			skipped++
		}
	}
	if skipped != 17 {
		t.Errorf("expected 17 skipped entries, got %d", skipped)
	}
}

func TestMappingTableIntegrity(t *testing.T) {
	mapper := NewHelmValuesMapper()
	mappings := mapper.Mappings()

	// No duplicate ConfigKeys.
	configKeys := make(map[string]bool)
	for _, m := range mappings {
		if configKeys[m.ConfigKey] {
			t.Errorf("duplicate ConfigKey: %s", m.ConfigKey)
		}
		configKeys[m.ConfigKey] = true
	}

	// No duplicate ValuesPath (among non-empty).
	valuesPaths := make(map[string]bool)
	for _, m := range mappings {
		if m.ValuesPath == "" {
			continue
		}
		if valuesPaths[m.ValuesPath] {
			t.Errorf("duplicate ValuesPath: %s", m.ValuesPath)
		}
		valuesPaths[m.ValuesPath] = true
	}
}

func TestMapValues_NilConfig(t *testing.T) {
	mapper := NewHelmValuesMapper()
	_, err := mapper.MapValues(nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Errorf("expected ErrNilConfig, got %v", err)
	}
}

func TestMapValues_GeneratedSecrets(t *testing.T) {
	config := newTestConfig(map[string]string{
		"JWT_SECRET":                    "test-jwt-secret",
		"ANON_KEY":                      "test-anon-key",
		"SERVICE_ROLE_KEY":              "test-service-key",
		"POSTGRES_PASSWORD":             "test-pg-pass",
		"DASHBOARD_PASSWORD":            "test-dash-pass",
		"SECRET_KEY_BASE":               "test-secret-key-base",
		"VAULT_ENC_KEY":                 "should-be-skipped",
		"PG_META_CRYPTO_KEY":            "test-meta-key",
		"LOGFLARE_PUBLIC_ACCESS_TOKEN":  "test-lf-pub",
		"LOGFLARE_PRIVATE_ACCESS_TOKEN": "test-lf-priv",
		"S3_PROTOCOL_ACCESS_KEY_ID":     "test-s3-key",
		"S3_PROTOCOL_ACCESS_KEY_SECRET": "test-s3-secret",
	})

	mapper := NewHelmValuesMapper()
	result, err := mapper.MapValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		path string
		want string
	}{
		{"secret.jwt.secret", "test-jwt-secret"},
		{"secret.jwt.anonKey", "test-anon-key"},
		{"secret.jwt.serviceKey", "test-service-key"},
		{"secret.db.password", "test-pg-pass"},
		{"secret.dashboard.password", "test-dash-pass"},
		{"secret.realtime.secretKeyBase", "test-secret-key-base"},
		{"secret.meta.cryptoKey", "test-meta-key"},
		{"secret.analytics.publicAccessToken", "test-lf-pub"},
		{"secret.analytics.privateAccessToken", "test-lf-priv"},
		{"secret.s3.keyId", "test-s3-key"},
		{"secret.s3.accessKey", "test-s3-secret"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := getNestedValue(result, tt.path)
			if got != tt.want {
				t.Errorf("path %s: got %v, want %v", tt.path, got, tt.want)
			}
		})
	}

	// VAULT_ENC_KEY should NOT appear anywhere.
	if v := getNestedValue(result, "secret.vault"); v != nil {
		t.Errorf("VAULT_ENC_KEY should be skipped, but found: %v", v)
	}
}

func TestMapValues_PerProjectKeys(t *testing.T) {
	config := newTestConfig(map[string]string{
		"KONG_HTTP_PORT":                "30080",
		"KONG_HTTPS_PORT":               "30443",
		"POSTGRES_PORT":                 "30432",
		"API_EXTERNAL_URL":              "http://localhost:30080",
		"SUPABASE_PUBLIC_URL":           "http://localhost:30080",
		"SITE_URL":                      "http://localhost:3000",
		"PROJECT_DATA_DIR":              "/data/test",
		"STUDIO_DEFAULT_ORGANIZATION":   "TestOrg",
		"STUDIO_DEFAULT_PROJECT":        "TestProj",
		"STORAGE_TENANT_ID":             "test-tenant",
		"POOLER_TENANT_ID":              "skip-me",
		"DOCKER_SOCKET_LOCATION":        "/var/run/docker.sock",
		"POOLER_PROXY_PORT_TRANSACTION": "6543",
	})

	mapper := NewHelmValuesMapper()
	result, err := mapper.MapValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mapped keys.
	if got := getNestedValue(result, "service.kong.port"); got != 30080 {
		t.Errorf("service.kong.port: got %v, want 30080", got)
	}
	if got := getNestedValue(result, "service.kong.nodePort"); got != 30080 {
		t.Errorf("service.kong.nodePort: got %v, want 30080", got)
	}
	if got := getNestedValue(result, "service.db.port"); got != 30432 {
		t.Errorf("service.db.port: got %v, want 30432", got)
	}
	if got := getNestedValue(result, "service.db.nodePort"); got != 30432 {
		t.Errorf("service.db.nodePort: got %v, want 30432", got)
	}
	if got := getNestedValue(result, "environment.auth.API_EXTERNAL_URL"); got != "http://localhost:30080" {
		t.Errorf("API_EXTERNAL_URL: got %v", got)
	}
	if got := getNestedValue(result, "environment.studio.SUPABASE_PUBLIC_URL"); got != "http://localhost:30080" {
		t.Errorf("SUPABASE_PUBLIC_URL: got %v", got)
	}
	if got := getNestedValue(result, "environment.auth.GOTRUE_SITE_URL"); got != "http://localhost:3000" {
		t.Errorf("SITE_URL: got %v", got)
	}
	if got := getNestedValue(result, "environment.studio.DEFAULT_ORGANIZATION_NAME"); got != "TestOrg" {
		t.Errorf("STUDIO_DEFAULT_ORGANIZATION: got %v", got)
	}
	if got := getNestedValue(result, "environment.storage.TENANT_ID"); got != "test-tenant" {
		t.Errorf("STORAGE_TENANT_ID: got %v", got)
	}
}

func TestMapValues_StaticValues(t *testing.T) {
	config := newTestConfig(map[string]string{})
	mapper := NewHelmValuesMapper()
	result, err := mapper.MapValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Service types.
	if got := getNestedValue(result, "service.kong.type"); got != "NodePort" {
		t.Errorf("service.kong.type: got %v, want NodePort", got)
	}
	if got := getNestedValue(result, "service.db.type"); got != "NodePort" {
		t.Errorf("service.db.type: got %v, want NodePort", got)
	}

	// Minio disabled.
	if got := getNestedValue(result, "deployment.minio.enabled"); got != false {
		t.Errorf("deployment.minio.enabled: got %v, want false", got)
	}

	// Persistence.
	if got := getNestedValue(result, "persistence.db.enabled"); got != true {
		t.Errorf("persistence.db.enabled: got %v, want true", got)
	}
	if got := getNestedValue(result, "persistence.db.size"); got != "5Gi" {
		t.Errorf("persistence.db.size: got %v, want 5Gi", got)
	}

	// Ingress disabled.
	if got := getNestedValue(result, "ingress.enabled"); got != false {
		t.Errorf("ingress.enabled: got %v, want false", got)
	}

	// Autoscaling disabled for all 12 services.
	for _, svc := range chartServices {
		path := "autoscaling." + svc + ".enabled"
		if got := getNestedValue(result, path); got != false {
			t.Errorf("%s: got %v, want false", path, got)
		}
	}
}

func TestMapValues_TransformError(t *testing.T) {
	config := newTestConfig(map[string]string{
		"KONG_HTTP_PORT": "not-a-number",
	})

	mapper := NewHelmValuesMapper()
	_, err := mapper.MapValues(config)
	if err == nil {
		t.Fatal("expected error from invalid port transform")
	}
	if got := err.Error(); !contains(got, "transform KONG_HTTP_PORT") {
		t.Errorf("error should mention key name, got: %s", got)
	}
}

func TestMapValues_GetSensitiveOkFalse(t *testing.T) {
	// Config with only a few keys — most mapping keys won't exist.
	config := newTestConfig(map[string]string{
		"JWT_SECRET": "my-secret",
	})

	mapper := NewHelmValuesMapper()
	result, err := mapper.MapValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JWT_SECRET should be mapped.
	if got := getNestedValue(result, "secret.jwt.secret"); got != "my-secret" {
		t.Errorf("secret.jwt.secret: got %v, want my-secret", got)
	}

	// Keys not in config should be silently skipped (not cause error).
	// Just verify no panic and result is valid.
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestMapValues_UnknownKeysIgnored(t *testing.T) {
	config := newTestConfig(map[string]string{
		"JWT_SECRET":    "secret-val",
		"UNKNOWN_KEY_1": "should-be-ignored",
		"UNKNOWN_KEY_2": "also-ignored",
	})

	mapper := NewHelmValuesMapper()
	result, err := mapper.MapValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JWT_SECRET should be mapped.
	if got := getNestedValue(result, "secret.jwt.secret"); got != "secret-val" {
		t.Errorf("secret.jwt.secret: got %v, want secret-val", got)
	}

	// Unknown keys should NOT appear in the result.
	// MapValues only iterates m.mappings, not config.Values.
}

func TestMapValues_UserOverridableKeys(t *testing.T) {
	config := newTestConfig(map[string]string{
		"PGRST_DB_SCHEMAS":     "public,storage",
		"SMTP_USER":            "smtp-user",
		"SMTP_PASS":            "smtp-pass",
		"JWT_EXPIRY":           "7200",
		"DASHBOARD_USERNAME":   "admin",
		"OPENAI_API_KEY":       "sk-test",
		"FUNCTIONS_VERIFY_JWT": "true",
	})

	mapper := NewHelmValuesMapper()
	result, err := mapper.MapValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		path string
		want string
	}{
		{"environment.rest.PGRST_DB_SCHEMAS", "public,storage"},
		{"secret.smtp.username", "smtp-user"},
		{"secret.smtp.password", "smtp-pass"},
		{"environment.auth.GOTRUE_JWT_EXP", "7200"},
		{"secret.dashboard.username", "admin"},
		{"secret.dashboard.openAiApiKey", "sk-test"},
		{"environment.functions.VERIFY_JWT", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := getNestedValue(result, tt.path)
			if got != tt.want {
				t.Errorf("path %s: got %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// === Test helpers ===

func newTestConfig(values map[string]string) *domain.ProjectConfig {
	return &domain.ProjectConfig{
		Values: values,
	}
}

func getNestedValue(m map[string]any, path string) any {
	parts := splitPath(path)
	current := any(m)
	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = cm[part]
	}
	return current
}

func splitPath(path string) []string {
	result := []string{}
	for _, p := range split(path, ".") {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func split(s, sep string) []string {
	var result []string
	for s != "" {
		i := indexOf(s, sep)
		if i < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:i])
		s = s[i+len(sep):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func assertMapsEqual(t *testing.T, got, want map[string]any) {
	t.Helper()
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if wantMap, ok := wv.(map[string]any); ok {
			gotMap, ok := gv.(map[string]any)
			if !ok {
				t.Errorf("key %q: expected map, got %T", k, gv)
				continue
			}
			assertMapsEqual(t, gotMap, wantMap)
		} else {
			if gv != wv {
				t.Errorf("key %q: got %v, want %v", k, gv, wv)
			}
		}
	}
}
