package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is injected at build time via -ldflags "-X main.version=x.y.z".
var version = "dev"

// ExitError bundles an exit code with the underlying error.
// RunE writes the human-readable message to stderr before returning ExitError.
// main() does NOT re-print the message; it only calls os.Exit with the code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func main() {
	root := buildRootCmd()
	if err := root.Execute(); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		// Fallback: any unwrapped error is treated as a user error.
		// RunE should always wrap with ExitError, so this path is a safety net.
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func buildRootCmd() *cobra.Command {
	var (
		dbURL       string
		projectsDir string
		output      string
		deps        *Deps
	)

	root := &cobra.Command{
		Use:     "sbctl",
		Short:   "Supabase Control Plane CLI",
		Version: version,
		// SilenceErrors prevents cobra from reprinting errors that RunE
		// already wrote to cmd.ErrOrStderr(). main() handles os.Exit only.
		SilenceErrors: true,
		// SilenceUsage prevents cobra from printing usage on every RunE error.
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&dbURL, "db-url",
		envOr("SBCTL_DB_URL", ""),
		"PostgreSQL DSN (env: SBCTL_DB_URL)")
	root.PersistentFlags().StringVar(&projectsDir, "projects-dir",
		envOr("SBCTL_PROJECTS_DIR", "./projects"),
		"Root directory for project files (env: SBCTL_PROJECTS_DIR)")
	root.PersistentFlags().StringVarP(&output, "output", "o", "table",
		"Output format: table|json|yaml")

	// PersistentPreRunE runs before every subcommand except --help/--version.
	// It validates flags and initialises the dependency graph via BuildDeps.
	//
	// ⚠️ Subcommands MUST NOT define their own PersistentPreRunE.
	// Cobra does NOT chain PersistentPreRunE — a subcommand's PersistentPreRunE
	// completely overwrites the parent's, causing BuildDeps to be silently
	// skipped (deps == nil → panic). If a subcommand needs pre-run logic,
	// extract a helper function and call it from both layers.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := validateOutput(output); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
			return &ExitError{Code: 1, Err: err}
		}
		if dbURL == "" {
			err := fmt.Errorf("--db-url or SBCTL_DB_URL is required")
			fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
			return &ExitError{Code: 1, Err: err}
		}
		var err error
		deps, err = BuildDeps(cmd.Context(), dbURL, projectsDir)
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
			return &ExitError{Code: 2, Err: err} // DB failure = system error
		}
		return nil
	}

	root.AddCommand(buildProjectCmd(&deps, &output))
	root.AddCommand(buildMCPCmd())

	return root
}

// validateOutput returns an error if the given output format is not supported.
func validateOutput(output string) error {
	switch output {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be table, json, or yaml", output)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
