package k8s

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// ErrNilConfig is returned when MapValues receives a nil ProjectConfig.
var ErrNilConfig = errors.New("helm values mapper: config must not be nil")

// HelmMapping describes how a single ProjectConfig key maps to a Helm values.yaml path.
type HelmMapping struct {
	// ConfigKey is the ProjectConfig key (e.g., "JWT_SECRET").
	ConfigKey string

	// ValuesPath is the dot-separated path in values.yaml (e.g., "secret.jwt.secret").
	// Empty string means this key is not mapped to values.yaml (skipped).
	ValuesPath string

	// Transform is an optional function to convert the config value before placing it
	// in the values map. If nil, the raw string value is used.
	Transform func(value string) (any, error)
}

// HelmValuesMapper converts a ProjectConfig into a Helm values map.
// It is stateless and safe for concurrent use.
type HelmValuesMapper struct {
	mappings []HelmMapping
}

// NewHelmValuesMapper creates a mapper with the default supabase chart mapping table.
func NewHelmValuesMapper() *HelmValuesMapper {
	return &HelmValuesMapper{
		mappings: defaultMappings(),
	}
}

// Mappings returns a copy of the mapping table for inspection (e.g., testing).
func (m *HelmValuesMapper) Mappings() []HelmMapping {
	cp := make([]HelmMapping, len(m.mappings))
	copy(cp, m.mappings)
	return cp
}

// MapValues converts a ProjectConfig into a nested map suitable for Helm values.yaml.
// Keys with empty ValuesPath are silently skipped.
// Returns error if any Transform function fails.
func (m *HelmValuesMapper) MapValues(config *domain.ProjectConfig) (map[string]any, error) {
	if config == nil {
		return nil, ErrNilConfig
	}

	result := make(map[string]any)

	// Inject static Helm values (K8s adapter fixed settings).
	injectStaticValues(result)

	// Iterate mapping entries.
	for _, mapping := range m.mappings {
		if mapping.ValuesPath == "" {
			continue
		}

		value, ok := config.GetSensitive(mapping.ConfigKey)
		if !ok {
			continue
		}

		var finalValue any = value
		if mapping.Transform != nil {
			transformed, err := mapping.Transform(value)
			if err != nil {
				return nil, fmt.Errorf("transform %s: %w", mapping.ConfigKey, err)
			}
			finalValue = transformed
		}

		setNestedValue(result, mapping.ValuesPath, finalValue)
	}

	// Set NodePort values from port config keys.
	if kongPort, ok := config.GetSensitive("KONG_HTTP_PORT"); ok {
		if v, err := strconv.Atoi(kongPort); err == nil {
			setNestedValue(result, "service.kong.nodePort", v)
		}
	}
	if dbPort, ok := config.GetSensitive("POSTGRES_PORT"); ok {
		if v, err := strconv.Atoi(dbPort); err == nil {
			setNestedValue(result, "service.db.nodePort", v)
		}
	}

	return result, nil
}

// chartServices lists all 12 chart services for autoscaling disable.
var chartServices = []string{
	"analytics", "auth", "db", "functions", "imgproxy", "kong",
	"meta", "realtime", "rest", "storage", "studio", "vector",
}

// injectStaticValues sets K8s adapter fixed settings into the values map.
func injectStaticValues(m map[string]any) {
	// Kong & DB use NodePort for external access.
	setNestedValue(m, "service.kong.type", "NodePort")
	setNestedValue(m, "service.db.type", "NodePort")

	// Disable minio (use file backend).
	setNestedValue(m, "deployment.minio.enabled", false)

	// Enable DB persistence.
	setNestedValue(m, "persistence.db.enabled", true)
	setNestedValue(m, "persistence.db.storageClassName", "")
	setNestedValue(m, "persistence.db.size", "5Gi")
	setNestedValue(m, "persistence.db.accessModes", []string{"ReadWriteOnce"})

	// Disable ingress (we use NodePort directly).
	setNestedValue(m, "ingress.enabled", false)

	// Disable autoscaling for all 12 services (local dev environment).
	for _, svc := range chartServices {
		setNestedValue(m, "autoscaling."+svc+".enabled", false)
	}
}

// toInt converts a string to int. Used as a Transform for port values.
func toInt(value string) (any, error) {
	v, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("expected integer: %w", err)
	}
	return v, nil
}

// setNestedValue sets a value in a nested map using a dot-separated path.
// Creates intermediate maps as needed. Existing intermediate maps are preserved.
func setNestedValue(m map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part]
		if !ok {
			newMap := make(map[string]any)
			current[part] = newMap
			current = newMap
			continue
		}
		nextMap, ok := next.(map[string]any)
		if !ok {
			newMap := make(map[string]any)
			current[part] = newMap
			current = newMap
			continue
		}
		current = nextMap
	}
}

// defaultMappings returns the complete mapping table for supabase-community/supabase-kubernetes chart v0.5.2.
// 93 entries total: 76 mapped, 17 skipped (empty ValuesPath).
func defaultMappings() []HelmMapping {
	return []HelmMapping{
		// === GeneratedSecret (12 keys: 11 mapped, 1 skipped) ===
		{ConfigKey: "JWT_SECRET", ValuesPath: "secret.jwt.secret"},
		{ConfigKey: "ANON_KEY", ValuesPath: "secret.jwt.anonKey"},
		{ConfigKey: "SERVICE_ROLE_KEY", ValuesPath: "secret.jwt.serviceKey"},
		{ConfigKey: "POSTGRES_PASSWORD", ValuesPath: "secret.db.password"},
		{ConfigKey: "DASHBOARD_PASSWORD", ValuesPath: "secret.dashboard.password"},
		{ConfigKey: "SECRET_KEY_BASE", ValuesPath: "secret.realtime.secretKeyBase"},
		{ConfigKey: "VAULT_ENC_KEY", ValuesPath: ""}, // supavisor — chart has no supavisor
		{ConfigKey: "PG_META_CRYPTO_KEY", ValuesPath: "secret.meta.cryptoKey"},
		{ConfigKey: "LOGFLARE_PUBLIC_ACCESS_TOKEN", ValuesPath: "secret.analytics.publicAccessToken"},
		{ConfigKey: "LOGFLARE_PRIVATE_ACCESS_TOKEN", ValuesPath: "secret.analytics.privateAccessToken"},
		{ConfigKey: "S3_PROTOCOL_ACCESS_KEY_ID", ValuesPath: "secret.s3.keyId"},
		{ConfigKey: "S3_PROTOCOL_ACCESS_KEY_SECRET", ValuesPath: "secret.s3.accessKey"},

		// === PerProject (13 keys: 8 mapped, 5 skipped) ===
		{ConfigKey: "KONG_HTTP_PORT", ValuesPath: "service.kong.port", Transform: toInt},
		{ConfigKey: "KONG_HTTPS_PORT", ValuesPath: ""},       // K8s doesn't need HTTPS port
		{ConfigKey: "POSTGRES_PORT", ValuesPath: "service.db.port", Transform: toInt},
		{ConfigKey: "POOLER_PROXY_PORT_TRANSACTION", ValuesPath: ""}, // chart has no supavisor
		{ConfigKey: "API_EXTERNAL_URL", ValuesPath: "environment.auth.API_EXTERNAL_URL"},
		{ConfigKey: "SUPABASE_PUBLIC_URL", ValuesPath: "environment.studio.SUPABASE_PUBLIC_URL"},
		{ConfigKey: "SITE_URL", ValuesPath: "environment.auth.GOTRUE_SITE_URL"},
		{ConfigKey: "PROJECT_DATA_DIR", ValuesPath: ""},      // K8s uses PVC
		{ConfigKey: "STUDIO_DEFAULT_ORGANIZATION", ValuesPath: "environment.studio.DEFAULT_ORGANIZATION_NAME"},
		{ConfigKey: "STUDIO_DEFAULT_PROJECT", ValuesPath: "environment.studio.DEFAULT_PROJECT_NAME"},
		{ConfigKey: "STORAGE_TENANT_ID", ValuesPath: "environment.storage.TENANT_ID"},
		{ConfigKey: "POOLER_TENANT_ID", ValuesPath: ""},      // chart has no supavisor
		{ConfigKey: "DOCKER_SOCKET_LOCATION", ValuesPath: ""}, // K8s doesn't use docker socket

		// === StaticDefault (38 keys: 32 mapped, 6 skipped) ===
		{ConfigKey: "POSTGRES_HOST", ValuesPath: ""},          // chart auto-configures pod DNS
		{ConfigKey: "POSTGRES_DB", ValuesPath: "secret.db.database"},
		{ConfigKey: "GOTRUE_API_HOST", ValuesPath: "environment.auth.GOTRUE_API_HOST"},
		{ConfigKey: "GOTRUE_API_PORT", ValuesPath: "environment.auth.GOTRUE_API_PORT"},
		{ConfigKey: "GOTRUE_DB_DRIVER", ValuesPath: "environment.auth.DB_DRIVER"},
		{ConfigKey: "GOTRUE_JWT_ADMIN_ROLES", ValuesPath: "environment.auth.GOTRUE_JWT_ADMIN_ROLES"},
		{ConfigKey: "GOTRUE_JWT_AUD", ValuesPath: "environment.auth.GOTRUE_JWT_AUD"},
		{ConfigKey: "GOTRUE_JWT_DEFAULT_GROUP_NAME", ValuesPath: "environment.auth.GOTRUE_JWT_DEFAULT_GROUP_NAME"},
		{ConfigKey: "PGRST_DB_ANON_ROLE", ValuesPath: "environment.rest.PGRST_DB_ANON_ROLE"},
		{ConfigKey: "PGRST_DB_USE_LEGACY_GUCS", ValuesPath: "environment.rest.PGRST_DB_USE_LEGACY_GUCS"},
		{ConfigKey: "KONG_DATABASE", ValuesPath: "environment.kong.KONG_DATABASE"},
		{ConfigKey: "KONG_DNS_ORDER", ValuesPath: "environment.kong.KONG_DNS_ORDER"},
		{ConfigKey: "KONG_DNS_NOT_FOUND_TTL", ValuesPath: ""}, // chart doesn't use this key
		{ConfigKey: "DB_AFTER_CONNECT_QUERY", ValuesPath: "environment.realtime.DB_AFTER_CONNECT_QUERY"},
		{ConfigKey: "DB_ENC_KEY", ValuesPath: "environment.realtime.DB_ENC_KEY"},
		{ConfigKey: "ERL_AFLAGS", ValuesPath: "environment.realtime.ERL_AFLAGS"},
		{ConfigKey: "APP_NAME", ValuesPath: "environment.realtime.APP_NAME"},
		{ConfigKey: "SEED_SELF_HOST", ValuesPath: "environment.realtime.SEED_SELF_HOST"},
		{ConfigKey: "RUN_JANITOR", ValuesPath: "environment.realtime.RUN_JANITOR"},
		{ConfigKey: "STORAGE_BACKEND", ValuesPath: ""},        // chart manages storage backend
		{ConfigKey: "FILE_SIZE_LIMIT", ValuesPath: "environment.storage.FILE_SIZE_LIMIT"},
		{ConfigKey: "ENABLE_IMAGE_TRANSFORMATION", ValuesPath: "environment.storage.ENABLE_IMAGE_TRANSFORMATION"},
		{ConfigKey: "IMGPROXY_LOCAL_FILESYSTEM_ROOT", ValuesPath: "environment.imgproxy.IMGPROXY_LOCAL_FILESYSTEM_ROOT"},
		{ConfigKey: "IMGPROXY_USE_ETAG", ValuesPath: "environment.imgproxy.IMGPROXY_USE_ETAG"},
		{ConfigKey: "LOGFLARE_NODE_HOST", ValuesPath: "environment.analytics.LOGFLARE_NODE_HOST"},
		{ConfigKey: "DB_SCHEMA", ValuesPath: "environment.analytics.DB_SCHEMA"},
		{ConfigKey: "LOGFLARE_SINGLE_TENANT", ValuesPath: "environment.analytics.LOGFLARE_SINGLE_TENANT"},
		{ConfigKey: "LOGFLARE_SUPABASE_MODE", ValuesPath: "environment.analytics.LOGFLARE_SUPABASE_MODE"},
		{ConfigKey: "CLUSTER_POSTGRES", ValuesPath: ""},       // supavisor only
		{ConfigKey: "REGION", ValuesPath: "environment.storage.REGION"},
		{ConfigKey: "POOLER_POOL_MODE", ValuesPath: ""},       // supavisor only
		{ConfigKey: "NEXT_PUBLIC_ENABLE_LOGS", ValuesPath: "environment.studio.NEXT_PUBLIC_ENABLE_LOGS"},
		{ConfigKey: "NEXT_ANALYTICS_BACKEND_PROVIDER", ValuesPath: "environment.studio.NEXT_ANALYTICS_BACKEND_PROVIDER"},
		{ConfigKey: "HOSTNAME", ValuesPath: ""},               // K8s auto-manages hostname
		{ConfigKey: "DISABLE_HEALTHCHECK_LOGGING", ValuesPath: "environment.realtime.DISABLE_HEALTHCHECK_LOGGING"},
		{ConfigKey: "REQUEST_ALLOW_X_FORWARDED_PATH", ValuesPath: "environment.storage.REQUEST_ALLOW_X_FORWARDED_PATH"},
		{ConfigKey: "PG_META_PORT", ValuesPath: "environment.meta.PG_META_PORT"},
		{ConfigKey: "IMGPROXY_BIND", ValuesPath: "environment.imgproxy.IMGPROXY_BIND"},

		// === UserOverridable (30 keys: 25 mapped, 5 skipped) ===
		{ConfigKey: "PGRST_DB_SCHEMAS", ValuesPath: "environment.rest.PGRST_DB_SCHEMAS"},
		{ConfigKey: "PGRST_DB_MAX_ROWS", ValuesPath: "environment.rest.PGRST_DB_MAX_ROWS"},
		{ConfigKey: "PGRST_DB_EXTRA_SEARCH_PATH", ValuesPath: "environment.rest.PGRST_DB_EXTRA_SEARCH_PATH"},
		{ConfigKey: "ADDITIONAL_REDIRECT_URLS", ValuesPath: "environment.auth.GOTRUE_URI_ALLOW_LIST"},
		{ConfigKey: "DISABLE_SIGNUP", ValuesPath: "environment.auth.GOTRUE_DISABLE_SIGNUP"},
		{ConfigKey: "ENABLE_EMAIL_SIGNUP", ValuesPath: "environment.auth.GOTRUE_EXTERNAL_EMAIL_ENABLED"},
		{ConfigKey: "ENABLE_ANONYMOUS_USERS", ValuesPath: "environment.auth.GOTRUE_EXTERNAL_ANONYMOUS_USERS_ENABLED"},
		{ConfigKey: "ENABLE_EMAIL_AUTOCONFIRM", ValuesPath: "environment.auth.GOTRUE_MAILER_AUTOCONFIRM"},
		{ConfigKey: "ENABLE_PHONE_SIGNUP", ValuesPath: "environment.auth.GOTRUE_EXTERNAL_PHONE_ENABLED"},
		{ConfigKey: "ENABLE_PHONE_AUTOCONFIRM", ValuesPath: "environment.auth.GOTRUE_SMS_AUTOCONFIRM"},
		{ConfigKey: "SMTP_ADMIN_EMAIL", ValuesPath: "environment.auth.GOTRUE_SMTP_ADMIN_EMAIL"},
		{ConfigKey: "SMTP_HOST", ValuesPath: "environment.auth.GOTRUE_SMTP_HOST"},
		{ConfigKey: "SMTP_PORT", ValuesPath: "environment.auth.GOTRUE_SMTP_PORT"},
		{ConfigKey: "SMTP_USER", ValuesPath: "secret.smtp.username"},
		{ConfigKey: "SMTP_PASS", ValuesPath: "secret.smtp.password"},
		{ConfigKey: "SMTP_SENDER_NAME", ValuesPath: "environment.auth.GOTRUE_SMTP_SENDER_NAME"},
		{ConfigKey: "MAILER_URLPATHS_INVITE", ValuesPath: "environment.auth.GOTRUE_MAILER_URLPATHS_INVITE"},
		{ConfigKey: "MAILER_URLPATHS_CONFIRMATION", ValuesPath: "environment.auth.GOTRUE_MAILER_URLPATHS_CONFIRMATION"},
		{ConfigKey: "MAILER_URLPATHS_RECOVERY", ValuesPath: "environment.auth.GOTRUE_MAILER_URLPATHS_RECOVERY"},
		{ConfigKey: "MAILER_URLPATHS_EMAIL_CHANGE", ValuesPath: "environment.auth.GOTRUE_MAILER_URLPATHS_EMAIL_CHANGE"},
		{ConfigKey: "FUNCTIONS_VERIFY_JWT", ValuesPath: "environment.functions.VERIFY_JWT"},
		{ConfigKey: "IMGPROXY_AUTO_WEBP", ValuesPath: ""},     // chart uses IMGPROXY_ENABLE_WEBP_DETECTION
		{ConfigKey: "IMGPROXY_MAX_SRC_RESOLUTION", ValuesPath: ""}, // chart doesn't use this key
		{ConfigKey: "GLOBAL_S3_BUCKET", ValuesPath: "environment.storage.GLOBAL_S3_BUCKET"},
		{ConfigKey: "OPENAI_API_KEY", ValuesPath: "secret.dashboard.openAiApiKey"},
		{ConfigKey: "POOLER_DEFAULT_POOL_SIZE", ValuesPath: ""}, // supavisor only
		{ConfigKey: "POOLER_MAX_CLIENT_CONN", ValuesPath: ""},   // supavisor only
		{ConfigKey: "POOLER_DB_POOL_SIZE", ValuesPath: ""},      // supavisor only
		{ConfigKey: "JWT_EXPIRY", ValuesPath: "environment.auth.GOTRUE_JWT_EXP"},
		{ConfigKey: "DASHBOARD_USERNAME", ValuesPath: "secret.dashboard.username"},
	}
}
