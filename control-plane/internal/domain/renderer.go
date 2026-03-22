package domain

// Artifact represents a single rendered configuration file.
type Artifact struct {
	// Path is the file path relative to the project directory.
	Path string
	// Content is the rendered file content.
	Content []byte
	// Mode is the Unix file permission bits (e.g. 0600 for secret files).
	Mode uint32
}

// ConfigRenderer renders a ProjectConfig into runtime-specific configuration artifacts.
// The Docker Compose Adapter renders to .env files; the K8s Adapter renders to ConfigMap+Secret YAML.
type ConfigRenderer interface {
	// Render converts a ProjectConfig into a set of Artifacts to be written to disk.
	Render(config *ProjectConfig) ([]Artifact, error)
}
