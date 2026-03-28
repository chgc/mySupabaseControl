package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagErrorWithSuggestions is a cobra FlagErrorFunc that appends a
// "did you mean --X?" hint when the user mistypes a flag name.
func flagErrorWithSuggestions(cmd *cobra.Command, err error) error {
	msg := err.Error()

	// cobra formats unknown-flag errors as: `unknown flag: --<name>`
	const prefix = "unknown flag: --"
	if strings.HasPrefix(msg, prefix) {
		typo := strings.TrimPrefix(msg, prefix)
		if suggestions := flagSuggestions(cmd, typo); len(suggestions) > 0 {
			hints := make([]string, len(suggestions))
			for i, s := range suggestions {
				hints[i] = "--" + s
			}
			msg = fmt.Sprintf("%s — did you mean %s?", msg, strings.Join(hints, " or "))
		}
	}

	return fmt.Errorf("%s", msg)
}

// flagSuggestions returns flag names (without "--") whose Levenshtein distance
// from typo is at most 2, searching both local and inherited flags of cmd.
func flagSuggestions(cmd *cobra.Command, typo string) []string {
	const maxDist = 2
	seen := map[string]bool{}
	var matches []string

	visit := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if levenshtein(typo, name) <= maxDist {
			matches = append(matches, name)
		}
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) { visit(f.Name) })
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) { visit(f.Name) })

	return matches
}

// levenshtein computes the edit distance between strings a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// dp[j] = edit distance between a[:i] and b[:j]
	dp := make([]int, lb+1)
	for j := range dp {
		dp[j] = j
	}

	for i := 1; i <= la; i++ {
		prev := i // dp[0] for this row = i deletions
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			next := min3(dp[j]+1, prev+1, dp[j-1]+cost)
			dp[j-1] = prev
			prev = next
		}
		dp[lb] = prev
	}
	return dp[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
