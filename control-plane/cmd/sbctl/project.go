package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/usecase"
)

// buildProjectCmd builds the "project" command group and all its subcommands.
// deps is a double pointer: *deps is nil at construction time and becomes valid
// after PersistentPreRunE runs. output likewise points to the global flag value.
func buildProjectCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage Supabase projects",
	}
	// ⚠️ Do NOT define PersistentPreRunE here — it would shadow the root
	// command's PersistentPreRunE and prevent BuildDeps from running.
	cmd.AddCommand(
		buildCreateCmd(deps, output, colorOut),
		buildListCmd(deps, output, colorOut),
		buildGetCmd(deps, output, colorOut),
		buildStartCmd(deps, output, colorOut),
		buildStopCmd(deps, output, colorOut),
		buildResetCmd(deps, output, colorOut),
		buildDeleteCmd(deps, output),
		buildCredentialsCmd(deps, output),
	)
	return cmd
}

func buildCreateCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
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
			if err := writeProjectView(cmd.OutOrStdout(), *output, view, *colorOut); err != nil {
				return err
			}
			if *output == "table" {
				creds, credErr := (*deps).ProjectService.GetCredentials(cmd.Context(), args[0])
				if credErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not retrieve credentials: %v\n", credErr)
				} else {
					writeCreateSummary(cmd.OutOrStdout(), creds)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&displayName, "display-name", "n", "", "Human-readable project name (required)")
	_ = cmd.MarkFlagRequired("display-name")
	cmd.Flags().StringVarP(&runtimeFlag, "runtime", "r", string(domain.RuntimeDockerCompose),
		"Runtime type: docker-compose or kubernetes")
	cmd.RegisterFlagCompletionFunc("runtime", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{
			string(domain.RuntimeDockerCompose) + "\tDocker Compose runtime",
			string(domain.RuntimeKubernetes) + "\tKubernetes runtime",
		}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func buildListCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	var watchCfg watchConfig
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all active Supabase projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if watchCfg.enabled {
				if *output != "table" {
					return &ExitError{Code: 1, Err: fmt.Errorf("--watch is only supported with table output format")}
				}
				ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
				defer stop()
				if watchCfg.timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, watchCfg.timeout)
					defer cancel()
				}
				renderFn := func(ctx context.Context) error {
					views, err := (*deps).ProjectService.List(ctx)
					if err != nil {
						return err
					}
					return writeProjectViews(cmd.OutOrStdout(), *output, views, *colorOut)
				}
				return runWatch(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), watchCfg, renderFn)
			}

			views, err := (*deps).ProjectService.List(cmd.Context())
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectViews(cmd.OutOrStdout(), *output, views, *colorOut)
		},
	}
	addWatchFlags(cmd, &watchCfg)
	return cmd
}

func buildGetCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	var watchCfg watchConfig
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Get details of a Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if watchCfg.enabled {
				if *output != "table" {
					return &ExitError{Code: 1, Err: fmt.Errorf("--watch is only supported with table output format")}
				}
				ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
				defer stop()
				if watchCfg.timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, watchCfg.timeout)
					defer cancel()
				}
				renderFn := func(ctx context.Context) error {
					view, err := (*deps).ProjectService.Get(ctx, args[0])
					if err != nil {
						return err
					}
					return writeProjectView(cmd.OutOrStdout(), *output, view, *colorOut)
				}
				return runWatch(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), watchCfg, renderFn)
			}

			view, err := (*deps).ProjectService.Get(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view, *colorOut)
		},
	}
	addWatchFlags(cmd, &watchCfg)
	cmd.ValidArgsFunction = projectSlugCompletion(deps)
	return cmd
}

func buildStartCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <slug>",
		Short: "Start a stopped Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Start(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view, *colorOut)
		},
	}
	cmd.ValidArgsFunction = projectSlugCompletion(deps)
	return cmd
}

func buildStopCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <slug>",
		Short: "Stop a running Supabase project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Stop(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view, *colorOut)
		},
	}
	cmd.ValidArgsFunction = projectSlugCompletion(deps)
	return cmd
}

func buildResetCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset <slug>",
		Short: "Reset a Supabase project (wipe data and re-provision)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := (*deps).ProjectService.Reset(cmd.Context(), args[0])
			if err != nil {
				return projectErr(cmd, err)
			}
			return writeProjectView(cmd.OutOrStdout(), *output, view, *colorOut)
		},
	}
	cmd.ValidArgsFunction = projectSlugCompletion(deps)
	return cmd
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
	cmd.ValidArgsFunction = projectSlugCompletion(deps)
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
	cmd := &cobra.Command{
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
	cmd.ValidArgsFunction = projectSlugCompletion(deps)
	return cmd
}
