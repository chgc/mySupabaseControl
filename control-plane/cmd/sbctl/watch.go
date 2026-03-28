package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

type watchConfig struct {
	enabled  bool
	interval time.Duration
	timeout  time.Duration
}

func addWatchFlags(cmd *cobra.Command, cfg *watchConfig) {
	cmd.Flags().BoolVarP(&cfg.enabled, "watch", "w", false, "Watch for changes and refresh periodically")
	cmd.Flags().DurationVar(&cfg.interval, "watch-interval", 2*time.Second, "Refresh interval in watch mode (minimum 500ms)")
	cmd.Flags().DurationVar(&cfg.timeout, "watch-timeout", 5*time.Minute, "Maximum watch duration (0 for no timeout)")
}

func clearScreen(w io.Writer) {
	fmt.Fprint(w, "\033[2J\033[H")
}

// runWatch runs a render function in a loop, clearing the screen between iterations.
// It stops when the context is cancelled (Ctrl+C / timeout).
// renderFn errors are printed to errW but do NOT stop the loop.
func runWatch(ctx context.Context, w, errW io.Writer, cfg watchConfig, renderFn func(ctx context.Context) error) error {
	// Enforce minimum interval
	if cfg.interval < 500*time.Millisecond {
		cfg.interval = 500 * time.Millisecond
	}

	for {
		clearScreen(w)
		if err := renderFn(ctx); err != nil {
			fmt.Fprintf(errW, "Error: %v\n", err)
		}
		fmt.Fprintf(w, "\nLast updated: %s | Ctrl+C to exit\n", time.Now().Format(time.RFC3339))

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.interval):
			// Next iteration
		}
	}
}
