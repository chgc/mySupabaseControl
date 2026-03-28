package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
