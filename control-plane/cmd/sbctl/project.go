package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/usecase"
)

// buildProjectCmd builds the "project" command group and all its subcommands.
// deps is a double pointer: *deps is nil at construction time and becomes valid
// after PersistentPreRunE runs. output likewise points to the global flag value.
func buildProjectCmd(deps **Deps, output *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage Supabase projects",
	}
	// ⚠️ Do NOT define PersistentPreRunE here — it would shadow the root
	// command's PersistentPreRunE and prevent BuildDeps from running.
	cmd.AddCommand(
		buildCreateCmd(deps, output),
		buildListCmd(deps, output),
		buildGetCmd(deps, output),
		buildStartCmd(deps, output),
		buildStopCmd(deps, output),
		buildResetCmd(deps, output),
		buildDeleteCmd(deps, output),
		buildCredentialsCmd(deps, output),
	)
	return cmd
}

func buildCreateCmd(deps **Deps, output *string) *cobra.Command {
	var displayName string
	var runtimeFlag string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create and start a new Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := domain.RuntimeType(runtimeFlag)
			if err := domain.ValidateRuntimeType(rt); err != nil {
				return projectErr(cmd, &usecase.UsecaseError{Code: usecase.ErrCodeInvalidInput, Message: err.Error(), Err: err})
			}
			view, err := (*deps).ProjectService.Create(cmd.Context(), args[0], displayName, rt)
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view)
		},
	}
	cmd.Flags().StringVarP(&displayName, "display-name", "n", "", "Human-readable project name (required)")
	_ = cmd.MarkFlagRequired("display-name")
	cmd.Flags().StringVarP(&runtimeFlag, "runtime", "r", string(domain.RuntimeDockerCompose),
		"Runtime type: docker-compose or kubernetes")
	return cmd
}

func buildListCmd(deps **Deps, output *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all active Supabase projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			views, err := (*deps).ProjectService.List(cmd.Context())
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectViews(cmd.OutOrStdout(), *output, views)
		},
	}
}

func buildGetCmd(deps **Deps, output *string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <slug>",
		Short: "Get details of a Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Get(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view)
		},
	}
}

func buildStartCmd(deps **Deps, output *string) *cobra.Command {
	return &cobra.Command{
		Use:   "start <slug>",
		Short: "Start a stopped Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Start(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view)
		},
	}
}

func buildStopCmd(deps **Deps, output *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <slug>",
		Short: "Stop a running Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Stop(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view)
		},
	}
}

func buildResetCmd(deps **Deps, output *string) *cobra.Command {
	return &cobra.Command{
		Use:   "reset <slug>",
		Short: "Reset a Supabase project (wipe data and re-provision)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Reset(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view)
		},
	}
}

func buildDeleteCmd(deps **Deps, output *string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete a Supabase project and destroy all its data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			if !yes {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"This will permanently delete project %q and all its data.\n", slug)
				fmt.Fprint(cmd.ErrOrStderr(), "Type the project slug to confirm: ")
				scanner := bufio.NewScanner(cmd.InOrStdin())
				scanner.Scan()
				confirmed := strings.TrimSpace(scanner.Text())
				if confirmed != slug {
					err := fmt.Errorf("confirmation did not match — aborting")
					fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
					return &ExitError{Code: 1, Err: err}
				}
			}
			_, err := (*deps).ProjectService.Delete(cmd.Context(), slug)
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeDeleteResult(cmd.OutOrStdout(), *output, slug)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// projectErr maps a usecase error to an ExitError and writes to stderr.
func projectErr(cmd *cobra.Command, err error) error {
	var ucErr *usecase.UsecaseError
	if errors.As(err, &ucErr) {
		switch ucErr.Code {
		case usecase.ErrCodeInternal:
			fmt.Fprintln(cmd.ErrOrStderr(), "Error: internal error:", ucErr.Message)
			return &ExitError{Code: 2, Err: ucErr}
		default:
			fmt.Fprintln(cmd.ErrOrStderr(), "Error:", ucErr.Message)
			return &ExitError{Code: 1, Err: ucErr}
		}
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
	return &ExitError{Code: 2, Err: err}
}

func buildCredentialsCmd(deps **Deps, output *string) *cobra.Command {
	return &cobra.Command{
		Use:   "credentials <slug>",
		Short: "Show admin credentials for a project (unmasked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			cv, err := (*deps).ProjectService.GetCredentials(cmd.Context(), slug)
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeCredentialsView(cmd.OutOrStdout(), *output, cv)
		},
	}
}
