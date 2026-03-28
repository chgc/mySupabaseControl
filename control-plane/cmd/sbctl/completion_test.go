package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kevin/supabase-control-plane/internal/usecase"
)

func TestCompletionBash(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	root.AddCommand(buildCompletionCmd())

	out, _, err := runCmd(t, root, []string{"completion", "bash"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "_sbctl") {
		t.Errorf("expected bash completion to contain _sbctl, got: %q", out[:min(len(out), 200)])
	}
}

func TestCompletionZsh(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	root.AddCommand(buildCompletionCmd())

	out, _, err := runCmd(t, root, []string{"completion", "zsh"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "#compdef") {
		t.Errorf("expected zsh completion to contain #compdef, got: %q", out[:min(len(out), 200)])
	}
}

func TestCompletionFish(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	root.AddCommand(buildCompletionCmd())

	out, _, err := runCmd(t, root, []string{"completion", "fish"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "complete") {
		t.Errorf("expected fish completion to contain 'complete', got: %q", out[:min(len(out), 200)])
	}
}

func TestIsCompletionCmd(t *testing.T) {
	root := &cobra.Command{Use: "sbctl"}
	comp := buildCompletionCmd()
	root.AddCommand(comp)
	other := &cobra.Command{Use: "other", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(other)

	// The "completion" command itself should match.
	if !isCompletionCmd(comp) {
		t.Error("expected isCompletionCmd to return true for completion cmd")
	}

	// Subcommands of completion (bash, zsh, fish) should match.
	for _, sub := range comp.Commands() {
		if !isCompletionCmd(sub) {
			t.Errorf("expected isCompletionCmd to return true for %q subcommand", sub.Name())
		}
	}

	// Non-completion commands should not match.
	if isCompletionCmd(other) {
		t.Error("expected isCompletionCmd to return false for 'other' cmd")
	}
	if isCompletionCmd(root) {
		t.Error("expected isCompletionCmd to return false for root cmd")
	}
}

func TestPersistentPreRunE_SkipsForCompletionCmd(t *testing.T) {
	// Use the real buildRootCmd so we exercise the actual PersistentPreRunE.
	// Without --db-url, PersistentPreRunE would fail for normal commands.
	// Completion commands should skip that validation entirely.
	root := buildRootCmd()

	out, _, err := runCmd(t, root, []string{"completion", "bash"}, "")
	if err != nil {
		t.Fatalf("expected no error for completion bash (should skip BuildDeps), got: %v", err)
	}
	if !strings.Contains(out, "_sbctl") {
		t.Errorf("expected bash completion output, got: %q", out[:min(len(out), 200)])
	}
}

func TestPersistentPreRunE_SkipsForShellCompRequest(t *testing.T) {
	// Simulate cobra's internal __complete command.
	root := buildRootCmd()

	// Create a command named __complete that records whether it ran.
	ran := false
	fakeComplete := &cobra.Command{
		Use:    cobra.ShellCompRequestCmd,
		Hidden: true,
		RunE: func(*cobra.Command, []string) error {
			ran = true
			return nil
		},
	}
	root.AddCommand(fakeComplete)

	_, _, err := runCmd(t, root, []string{cobra.ShellCompRequestCmd}, "")
	if err != nil {
		t.Fatalf("expected no error for %s (should skip BuildDeps), got: %v", cobra.ShellCompRequestCmd, err)
	}
	if !ran {
		t.Error("expected __complete command to have run")
	}
}

func TestProjectSlugCompletion_ReturnsSlugs(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				{Slug: "alpha", DisplayName: "Alpha Project"},
				{Slug: "beta", DisplayName: "Beta Project"},
			}, nil
		},
	}}
	fn := projectSlugCompletion(&deps)

	completions, directive := fn(&cobra.Command{}, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != 2 {
		t.Fatalf("expected 2 completions, got %d: %v", len(completions), completions)
	}
	for _, want := range []string{"alpha\tAlpha Project", "beta\tBeta Project"} {
		found := false
		for _, c := range completions {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected completion %q, got %v", want, completions)
		}
	}
}

func TestProjectSlugCompletion_FiltersByPrefix(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				{Slug: "alpha", DisplayName: "Alpha"},
				{Slug: "beta", DisplayName: "Beta"},
				{Slug: "alpha-two", DisplayName: "Alpha Two"},
			}, nil
		},
	}}
	fn := projectSlugCompletion(&deps)

	completions, _ := fn(&cobra.Command{}, nil, "al")
	if len(completions) != 2 {
		t.Fatalf("expected 2 completions for prefix 'al', got %d: %v", len(completions), completions)
	}
	for _, c := range completions {
		if !strings.HasPrefix(c, "alpha") {
			t.Errorf("unexpected completion %q for prefix 'al'", c)
		}
	}
}

func TestProjectSlugCompletion_NilDeps_NoPanic(t *testing.T) {
	// When deps is nil (completion context, PersistentPreRunE skipped),
	// should return empty list without panicking.
	var deps *Deps
	fn := projectSlugCompletion(&deps)

	completions, directive := fn(&cobra.Command{}, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if completions != nil {
		t.Errorf("expected nil completions with nil deps, got %v", completions)
	}
}

func TestProjectSlugCompletion_ListError_ReturnsEmpty(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return nil, fmt.Errorf("db down")
		},
	}}
	fn := projectSlugCompletion(&deps)

	completions, directive := fn(&cobra.Command{}, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if completions != nil {
		t.Errorf("expected nil completions on error, got %v", completions)
	}
}

func TestOutputFlagCompletion(t *testing.T) {
	root := buildRootCmd()
	// Use cobra's __complete mechanism to test flag completion.
	// "sbctl --output <TAB>" translates to "__complete --output ''"
	out, _, err := runCmd(t, root, []string{cobra.ShellCompRequestCmd, "--output", ""}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"table", "json", "yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output flag completion, got: %q", want, out)
		}
	}
}

func TestRuntimeFlagCompletion(t *testing.T) {
	root := buildRootCmd()
	// "sbctl project create --runtime <TAB>"
	out, _, err := runCmd(t, root, []string{cobra.ShellCompRequestCmd, "project", "create", "--runtime", ""}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"docker-compose", "kubernetes"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in runtime flag completion, got: %q", want, out)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
