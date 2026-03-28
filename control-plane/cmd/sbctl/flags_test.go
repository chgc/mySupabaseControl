package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ── levenshtein ───────────────────────────────────────────────────────────────

func TestLevenshtein_Equal(t *testing.T) {
	if d := levenshtein("runtime", "runtime"); d != 0 {
		t.Fatalf("equal strings: want 0, got %d", d)
	}
}

func TestLevenshtein_OneDeletion(t *testing.T) {
	// "runtim" vs "runtime" — 1 insertion needed
	if d := levenshtein("runtim", "runtime"); d != 1 {
		t.Fatalf("want 1, got %d", d)
	}
}

func TestLevenshtein_OneSubstitution(t *testing.T) {
	if d := levenshtein("runtyme", "runtime"); d != 1 {
		t.Fatalf("want 1, got %d", d)
	}
}

func TestLevenshtein_TwoEdits(t *testing.T) {
	if d := levenshtein("runtim", "runtyme"); d != 2 {
		t.Fatalf("want 2, got %d", d)
	}
}

func TestLevenshtein_EmptyStrings(t *testing.T) {
	if d := levenshtein("", "abc"); d != 3 {
		t.Fatalf("want 3, got %d", d)
	}
	if d := levenshtein("abc", ""); d != 3 {
		t.Fatalf("want 3, got %d", d)
	}
	if d := levenshtein("", ""); d != 0 {
		t.Fatalf("want 0, got %d", d)
	}
}

func TestLevenshtein_TotallyDifferent(t *testing.T) {
	// distance > 2 — should NOT suggest
	if d := levenshtein("xyz", "runtime"); d <= 2 {
		t.Fatalf("totally different strings should have distance > 2, got %d", d)
	}
}

// ── flagSuggestions ───────────────────────────────────────────────────────────

func newTestCmd(flags ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	for _, f := range flags {
		cmd.Flags().String(f, "", "")
	}
	return cmd
}

func TestFlagSuggestions_ExactTypo(t *testing.T) {
	cmd := newTestCmd("runtime", "display-name", "output")
	got := flagSuggestions(cmd, "runtim") // missing 'e'
	if len(got) == 0 {
		t.Fatal("expected suggestion for 'runtim', got none")
	}
	if got[0] != "runtime" {
		t.Fatalf("expected 'runtime', got %q", got[0])
	}
}

func TestFlagSuggestions_NoMatch(t *testing.T) {
	cmd := newTestCmd("runtime", "display-name")
	got := flagSuggestions(cmd, "xyz")
	if len(got) != 0 {
		t.Fatalf("expected no suggestions for 'xyz', got %v", got)
	}
}

func TestFlagSuggestions_MultipleMatches(t *testing.T) {
	cmd := newTestCmd("run", "runtim", "runtime")
	got := flagSuggestions(cmd, "runtim")
	// "runtim" itself (dist=0), "runtime" (dist=1), "run" (dist=2) all qualify
	if len(got) < 2 {
		t.Fatalf("expected at least 2 matches, got %v", got)
	}
}

func TestFlagSuggestions_InheritedFlags(t *testing.T) {
	parent := &cobra.Command{Use: "root"}
	parent.PersistentFlags().String("output", "", "")

	child := &cobra.Command{Use: "sub"}
	parent.AddCommand(child)
	// Inherited flags are visible via child.InheritedFlags() only after AddCommand.
	got := flagSuggestions(child, "outpu") // missing 't'
	if len(got) == 0 {
		t.Fatal("expected suggestion for inherited flag 'output', got none")
	}
}

// ── flagErrorWithSuggestions (integration) ────────────────────────────────────

func TestFlagErrorWithSuggestions_UnknownFlagTypo(t *testing.T) {
	cmd := newTestCmd("runtime", "display-name")
	baseErr := fmt.Errorf("unknown flag: --runtim")
	result := flagErrorWithSuggestions(cmd, baseErr)
	if result == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(result.Error(), "did you mean") {
		t.Fatalf("expected 'did you mean' hint, got: %s", result.Error())
	}
	if !strings.Contains(result.Error(), "--runtime") {
		t.Fatalf("expected '--runtime' in hint, got: %s", result.Error())
	}
}

func TestFlagErrorWithSuggestions_NonTypoError(t *testing.T) {
	cmd := newTestCmd("runtime")
	baseErr := fmt.Errorf("flag needs an argument: --runtime")
	result := flagErrorWithSuggestions(cmd, baseErr)
	// Should return original message unchanged (no "unknown flag" prefix).
	if result.Error() != baseErr.Error() {
		t.Fatalf("non-typo error should be unchanged, got: %s", result.Error())
	}
}

func TestFlagErrorWithSuggestions_NoSuggestions(t *testing.T) {
	cmd := newTestCmd("runtime")
	baseErr := fmt.Errorf("unknown flag: --xyz")
	result := flagErrorWithSuggestions(cmd, baseErr)
	// No close match — message returned as-is (no "did you mean").
	if strings.Contains(result.Error(), "did you mean") {
		t.Fatalf("expected no suggestion for '--xyz', got: %s", result.Error())
	}
}
