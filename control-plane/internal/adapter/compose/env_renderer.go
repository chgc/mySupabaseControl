package compose

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// ComposeEnvRenderer implements domain.ConfigRenderer by producing a Docker Compose
// compatible .env file from a ProjectConfig.
//
// The output is a single Artifact:
//   - Path:    ".env" (relative; the adapter prepends the project directory)
//   - Content: UTF-8 key=value pairs, one per line, keys sorted lexicographically
//   - Mode:    0600 (owner-read/write only, protects secrets)
//
// Render uses GetSensitive to obtain real values (not masked "***").
// Values containing control characters (\n or \r) are rejected with an error.
// config.Overrides does not need special handling because ResolveConfig already
// merges overrides into Values before Render is called.
type ComposeEnvRenderer struct{}

// Static interface assertion.
var _ domain.ConfigRenderer = (*ComposeEnvRenderer)(nil)

// NewComposeEnvRenderer returns a stateless ComposeEnvRenderer.
func NewComposeEnvRenderer() *ComposeEnvRenderer {
	return &ComposeEnvRenderer{}
}

// Render converts config into a .env Artifact suitable for Docker Compose v2.
//
// Returns an error if:
//   - config is nil
//   - config.Values is nil
//   - any value contains \n or \r (control characters incompatible with .env format)
func (r *ComposeEnvRenderer) Render(config *domain.ProjectConfig) ([]domain.Artifact, error) {
	if config == nil {
		return nil, fmt.Errorf("env renderer: config is nil")
	}
	if config.Values == nil {
		return nil, fmt.Errorf("env renderer: config.Values is nil")
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(config.Values))
	for k := range config.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		v, _ := config.GetSensitive(k)

		// Reject values containing control characters.
		if strings.ContainsAny(v, "\n\r") {
			return nil, fmt.Errorf("env renderer: key %q value contains control character", k)
		}

		escaped, quoted := escapeValue(v)
		if quoted {
			fmt.Fprintf(&sb, "%s=\"%s\"\n", k, escaped)
		} else {
			fmt.Fprintf(&sb, "%s=%s\n", k, escaped)
		}
	}

	return []domain.Artifact{
		{
			Path:    ".env",
			Content: []byte(sb.String()),
			Mode:    0600,
		},
	}, nil
}

// escapeValue prepares a value for .env output.
// Returns the escaped string and whether it needs double-quoting.
//
// Quoting is triggered when the value contains: #, space, tab, $, \, or "
// Escape order (must be applied in this order to prevent double-escaping):
//  1. \ → \\ (must be first)
//  2. " → \"
//  3. $ → \$ (prevents godotenv variable interpolation inside double-quoted strings)
func escapeValue(v string) (escaped string, needsQuote bool) {
	needsQuote = strings.ContainsAny(v, "#\t $\\\"")

	if !needsQuote {
		return v, false
	}

	// Step 1: escape backslashes first.
	s := strings.ReplaceAll(v, `\`, `\\`)
	// Step 2: escape double quotes.
	s = strings.ReplaceAll(s, `"`, `\"`)
	// Step 3: escape dollar signs (prevents godotenv interpolation).
	s = strings.ReplaceAll(s, `$`, `\$`)

	return s, true
}
