package domain

// ConfigCategory classifies how an environment variable is managed.
type ConfigCategory string

const (
	// CategoryStaticDefault applies to variables that are the same across all projects.
	CategoryStaticDefault ConfigCategory = "static_default"
	// CategoryPerProject applies to variables computed or allocated per project (e.g. ports, URLs).
	CategoryPerProject ConfigCategory = "per_project"
	// CategoryGeneratedSecret applies to per-project secrets generated automatically at creation.
	CategoryGeneratedSecret ConfigCategory = "generated_secret"
	// CategoryUserOverridable applies to variables that have defaults but can be overridden by the user.
	CategoryUserOverridable ConfigCategory = "user_overridable"
)

// ConfigScope defines whether a configuration value is shared across the host or per-project.
type ConfigScope string

const (
	// ScopeGlobal means the value is shared across all projects on the host.
	ScopeGlobal ConfigScope = "global"
	// ScopeProject means each project has its own independent value.
	ScopeProject ConfigScope = "project"
)

// ConfigEntry holds metadata for a single environment variable.
type ConfigEntry struct {
	// Key is the environment variable name.
	Key string
	// Category determines how the value is sourced.
	Category ConfigCategory
	// Scope determines whether the value is host-wide or per-project.
	Scope ConfigScope
	// DefaultValue is the built-in default (used for StaticDefault and UserOverridable).
	DefaultValue string
	// Description is a human-readable explanation of the variable's purpose.
	Description string
	// Services lists which Supabase stack services consume this variable.
	Services []ServiceName
	// Sensitive marks variables whose values must be masked in logs and UI responses.
	Sensitive bool
	// Required marks variables that must have a resolved value; missing ones cause ErrMissingRequiredConfig.
	Required bool
}

// PortSet holds the full set of ports allocated for a single project.
// KONG_HTTPS_PORT is deliberately absent — it is a derived value (KongHTTP + 1)
// computed by computePerProjectVars and never independently allocated.
type PortSet struct {
	KongHTTP     int // External API port (starting from 28081).
	PostgresPort int // PostgreSQL port (starting from 54320).
	PoolerPort   int // Supavisor transaction port (starting from 64300).
	StudioPort   int // Studio UI port (starting from 54323).
	MetaPort     int // pg-meta API port (starting from 54380).
	ImgProxyPort int // imgproxy listen port (starting from 54381).
}
