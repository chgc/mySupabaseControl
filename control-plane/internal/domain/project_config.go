package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// ProjectConfig holds the fully-resolved configuration for a single project.
// It is produced by ResolveConfig and consumed by ConfigRenderer implementations.
type ProjectConfig struct {
	// ProjectSlug identifies which project this config belongs to.
	ProjectSlug string

	// Values contains all resolved env-var key→value pairs (including sensitive ones).
	Values map[string]string

	// Overrides records the user-supplied overrides (UserOverridable keys only).
	Overrides map[string]string
}

// Get returns the value for the given key.
// For Sensitive=true entries it returns the masked value "***" to prevent
// accidental exposure in logs or API responses.
// Use GetSensitive when the real value is needed (e.g. rendering .env).
func (c *ProjectConfig) Get(key string) (string, bool) {
	val, ok := c.Values[key]
	if !ok {
		return "", false
	}
	if isSensitiveKey(key) {
		return "***", true
	}
	return val, true
}

// GetSensitive returns the raw value for the given key, including sensitive ones.
// Use only in contexts where the real value is required (e.g. ConfigRenderer.Render).
func (c *ProjectConfig) GetSensitive(key string) (string, bool) {
	val, ok := c.Values[key]
	return val, ok
}

// ResolveConfig merges config schema defaults, per-project computed values,
// generated secrets, and user overrides into a fully resolved ProjectConfig.
//
// Priority (highest → lowest):
//  1. User overrides (UserOverridable keys only)
//  2. Per-project computed values (PerProject keys, via computePerProjectVars)
//  3. Generated secrets (GeneratedSecret keys, from the secrets map)
//  4. Static defaults (StaticDefault keys, from ConfigEntry.DefaultValue)
//
// Returns ErrConfigNotOverridable if any key in overrides is not UserOverridable.
// Returns ErrMissingRequiredConfig if any Required entry has no resolved value.
func ResolveConfig(
	project *ProjectModel,
	secrets map[string]string,
	portSet *PortSet,
	overrides map[string]string,
) (*ProjectConfig, error) {
	schema := ConfigSchema()

	// Build a lookup map for quick category checks.
	entryByKey := make(map[string]ConfigEntry, len(schema))
	for _, e := range schema {
		entryByKey[e.Key] = e
	}

	// Validate overrides — only UserOverridable keys are allowed.
	for k := range overrides {
		e, ok := entryByKey[k]
		if !ok || e.Category != CategoryUserOverridable {
			return nil, &ErrConfigNotOverridable{Key: k}
		}
	}

	if portSet == nil {
		return nil, &ErrInvalidPortSet{Key: "portSet"}
	}

	perProject := computePerProjectVars(project, portSet)

	values := make(map[string]string, len(schema))

	for _, entry := range schema {
		var val string
		switch entry.Category {
		case CategoryStaticDefault:
			val = entry.DefaultValue
		case CategoryPerProject:
			val = perProject[entry.Key]
		case CategoryGeneratedSecret:
			val = secrets[entry.Key]
		case CategoryUserOverridable:
			if ov, ok := overrides[entry.Key]; ok {
				val = ov
			} else {
				val = entry.DefaultValue
			}
		}
		if val != "" || !entry.Required {
			values[entry.Key] = val
		}
	}

	// Check required keys.
	var missing []string
	for _, entry := range schema {
		if entry.Required {
			if v, ok := values[entry.Key]; !ok || v == "" {
				missing = append(missing, entry.Key)
			}
		}
	}
	if len(missing) > 0 {
		return nil, &ErrMissingRequiredConfig{Keys: missing}
	}

	// Copy overrides (only actually-applied ones).
	appliedOverrides := make(map[string]string, len(overrides))
	for k, v := range overrides {
		appliedOverrides[k] = v
	}

	return &ProjectConfig{
		ProjectSlug: project.Slug,
		Values:      values,
		Overrides:   appliedOverrides,
	}, nil
}

// computePerProjectVars calculates all PerProject env-var values from the
// ProjectModel and allocated PortSet.
// KONG_HTTPS_PORT is a derived value (KongHTTP + 1) computed here; it is not
// stored in PortSet and not read from it.
func computePerProjectVars(project *ProjectModel, ports *PortSet) map[string]string {
	kongHTTP := strconv.Itoa(ports.KongHTTP)
	return map[string]string{
		"KONG_HTTP_PORT":               kongHTTP,
		"KONG_HTTPS_PORT":              strconv.Itoa(ports.KongHTTP + 1),
		"POSTGRES_PORT":                strconv.Itoa(ports.PostgresPort),
		"POOLER_PROXY_PORT_TRANSACTION": strconv.Itoa(ports.PoolerPort),
		"API_EXTERNAL_URL":             "http://localhost:" + kongHTTP,
		"SUPABASE_PUBLIC_URL":          "http://localhost:" + kongHTTP,
		"SITE_URL":                     "http://localhost:3000",
		"PROJECT_DATA_DIR":             "./projects/" + project.Slug + "/volumes",
		"STUDIO_DEFAULT_ORGANIZATION":  "Default Organization",
		"STUDIO_DEFAULT_PROJECT":       project.DisplayName,
		"STORAGE_TENANT_ID":            "stub",
		"POOLER_TENANT_ID":             project.Slug,
		"DOCKER_SOCKET_LOCATION":       "/var/run/docker.sock",
		"STUDIO_PORT":                  strconv.Itoa(ports.StudioPort),
		"PG_META_PORT":                 strconv.Itoa(ports.MetaPort),
		"IMGPROXY_BIND":                fmt.Sprintf(":%d", ports.ImgProxyPort),
	}
}

// ExtractSecrets extracts GeneratedSecret key→value pairs from an existing
// ProjectConfig. Used when loading an existing project to avoid re-generating secrets.
func ExtractSecrets(config *ProjectConfig) map[string]string {
	secrets := make(map[string]string)
	for _, entry := range ConfigSchema() {
		if entry.Category == CategoryGeneratedSecret {
			if val, ok := config.Values[entry.Key]; ok {
				secrets[entry.Key] = val
			}
		}
	}
	return secrets
}

// ExtractPortSet reconstructs a PortSet from an existing ProjectConfig's Values.
// Used when loading an existing project from the ConfigRepository.
// Returns ErrInvalidPortSet if any required port key is missing or unparseable.
func ExtractPortSet(config *ProjectConfig) (*PortSet, error) {
	parseInt := func(key string) (int, error) {
		raw, ok := config.Values[key]
		if !ok || raw == "" {
			return 0, &ErrInvalidPortSet{Key: key}
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return 0, &ErrInvalidPortSet{Key: key, Value: raw}
		}
		return v, nil
	}

	kongHTTP, err := parseInt("KONG_HTTP_PORT")
	if err != nil {
		return nil, err
	}
	postgresPort, err := parseInt("POSTGRES_PORT")
	if err != nil {
		return nil, err
	}
	poolerPort, err := parseInt("POOLER_PROXY_PORT_TRANSACTION")
	if err != nil {
		return nil, err
	}
	studioPort, err := parseInt("STUDIO_PORT")
	if err != nil {
		return nil, err
	}
	metaPort, err := parseInt("PG_META_PORT")
	if err != nil {
		return nil, err
	}

	// IMGPROXY_BIND is stored as ":{port}" — strip the leading colon.
	imgproxyRaw, ok := config.Values["IMGPROXY_BIND"]
	if !ok || imgproxyRaw == "" {
		return nil, &ErrInvalidPortSet{Key: "IMGPROXY_BIND"}
	}
	imgproxyStr := strings.TrimPrefix(imgproxyRaw, ":")
	imgProxyPort, err := strconv.Atoi(imgproxyStr)
	if err != nil {
		return nil, &ErrInvalidPortSet{Key: "IMGPROXY_BIND", Value: imgproxyRaw}
	}

	return &PortSet{
		KongHTTP:     kongHTTP,
		PostgresPort: postgresPort,
		PoolerPort:   poolerPort,
		StudioPort:   studioPort,
		MetaPort:     metaPort,
		ImgProxyPort: imgProxyPort,
	}, nil
}

// sensitiveKeys is the set of keys whose values are masked by Get().
// Populated once at init time from ConfigSchema() to avoid repeated scans.
var sensitiveKeys map[string]bool

func init() {
	sensitiveKeys = make(map[string]bool)
	for _, e := range ConfigSchema() {
		if e.Sensitive {
			sensitiveKeys[e.Key] = true
		}
	}
}

func isSensitiveKey(key string) bool {
	return sensitiveKeys[key]
}
