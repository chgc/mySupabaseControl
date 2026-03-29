package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

type colorer struct {
	enabled bool
}

// newColorer creates a colorer. Priority: --no-color flag > NO_COLOR env > TTY detection.
// Uses os.LookupEnv for NO_COLOR (existence matters, not value per no-color.org spec).
func newColorer(fd uintptr, noColorFlag bool) *colorer {
	if noColorFlag {
		return &colorer{enabled: false}
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return &colorer{enabled: false}
	}
	return &colorer{enabled: term.IsTerminal(int(fd))}
}

// status wraps a project status string with ANSI color codes.
// Uses \033[39m (default foreground) as reset to maintain consistent 10-byte overhead for tabwriter alignment.
func (c *colorer) status(s string) string {
	if c == nil || !c.enabled {
		return s
	}
	var code string
	switch s {
	case "running":
		code = "\033[32m" // Green
	case "creating", "starting", "stopping", "destroying":
		code = "\033[33m" // Yellow
	case "stopped", "destroyed":
		code = "\033[90m" // Dark gray
	case "error":
		code = "\033[31m" // Red
	default:
		code = "\033[39m" // Default foreground (same 5 bytes as other codes)
	}
	return fmt.Sprintf("%s%s\033[39m", code, s)
}
