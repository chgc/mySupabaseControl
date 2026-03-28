package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// projectSlugCompletion returns a Cobra ValidArgsFunction that completes
// project slugs. When deps is nil (completion context where PersistentPreRunE
// is skipped), it returns an empty list gracefully.
func projectSlugCompletion(deps **Deps) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		directive := cobra.ShellCompDirectiveNoFileComp
		if deps == nil || *deps == nil {
			return nil, directive
		}
		views, err := (*deps).ProjectService.List(cmd.Context())
		if err != nil {
			return nil, directive
		}
		var completions []string
		for _, v := range views {
			if toComplete == "" || strings.HasPrefix(v.Slug, toComplete) {
				completions = append(completions, fmt.Sprintf("%s\t%s", v.Slug, v.DisplayName))
			}
		}
		return completions, directive
	}
}

func buildCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts",
		Long:  "Generate completion scripts for bash, zsh, or fish shells.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "bash",
			Short: "Generate bash completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
			},
		},
		&cobra.Command{
			Use:   "zsh",
			Short: "Generate zsh completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "fish",
			Short: "Generate fish completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			},
		},
	)
	return cmd
}

// isCompletionCmd checks if cmd or any of its parents is the "completion" command.
func isCompletionCmd(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "completion" {
			return true
		}
	}
	return false
}
