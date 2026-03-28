package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWatch_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var buf, errBuf bytes.Buffer
	cfg := watchConfig{interval: 50 * time.Millisecond}

	done := make(chan error, 1)
	go func() {
		done <- runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
			return nil
		})
	}()

	// Let at least one render happen, then cancel.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runWatch did not stop promptly after context cancellation")
	}
}

func TestRunWatch_TimeoutStopsLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	var buf, errBuf bytes.Buffer
	cfg := watchConfig{interval: 20 * time.Millisecond}

	start := time.Now()
	err := runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
		return nil
	})
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "should stop near the timeout, not run forever")
}

func TestRunWatch_RenderError_PrintsAndContinues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	var buf, errBuf bytes.Buffer
	var callCount atomic.Int32

	cfg := watchConfig{interval: 500 * time.Millisecond}

	err := runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
		n := callCount.Add(1)
		if n == 1 {
			return fmt.Errorf("render failed")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Contains(t, errBuf.String(), "Error: render failed")
	assert.Greater(t, int(callCount.Load()), 1, "loop should continue after render error")
}

func TestRunWatch_ClearScreenCalled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf, errBuf bytes.Buffer
	cfg := watchConfig{interval: 20 * time.Millisecond}

	_ = runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
		return nil
	})

	assert.Contains(t, buf.String(), "\033[2J\033[H", "output should contain ANSI clear-screen sequence")
}

func TestRunWatch_TimestampLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf, errBuf bytes.Buffer
	cfg := watchConfig{interval: 20 * time.Millisecond}

	_ = runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
		return nil
	})

	assert.Contains(t, buf.String(), "Last updated:")
	assert.Contains(t, buf.String(), "Ctrl+C to exit")
}

func TestAddWatchFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var cfg watchConfig
	addWatchFlags(cmd, &cfg)

	watchFlag := cmd.Flags().Lookup("watch")
	require.NotNil(t, watchFlag, "--watch flag should be registered")
	assert.Equal(t, "w", watchFlag.Shorthand)
	assert.Equal(t, "false", watchFlag.DefValue)

	intervalFlag := cmd.Flags().Lookup("watch-interval")
	require.NotNil(t, intervalFlag, "--watch-interval flag should be registered")
	assert.Equal(t, "2s", intervalFlag.DefValue)

	timeoutFlag := cmd.Flags().Lookup("watch-timeout")
	require.NotNil(t, timeoutFlag, "--watch-timeout flag should be registered")
	assert.Equal(t, "5m0s", timeoutFlag.DefValue)
}

func TestRunWatch_MinimumIntervalEnforced(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var buf, errBuf bytes.Buffer
	var callCount atomic.Int32

	// Set interval way below minimum; should be clamped to 500ms.
	cfg := watchConfig{interval: 10 * time.Millisecond}

	start := time.Now()
	_ = runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
		callCount.Add(1)
		return nil
	})
	elapsed := time.Since(start)

	// With 200ms timeout and 500ms clamped interval, we expect exactly 1 render
	// (the first render happens immediately, then the 500ms wait is interrupted
	// by the 200ms timeout).
	calls := int(callCount.Load())
	assert.Equal(t, 1, calls, "with 500ms minimum interval and 200ms timeout, only 1 render should occur")
	assert.Less(t, elapsed, 600*time.Millisecond, "should stop at timeout, not wait for full interval")
}

func TestClearScreen(t *testing.T) {
	var buf bytes.Buffer
	clearScreen(&buf)
	assert.Equal(t, "\033[2J\033[H", buf.String())
}

func TestRunWatch_RenderFnReceivesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf, errBuf bytes.Buffer
	cfg := watchConfig{interval: 50 * time.Millisecond}

	done := make(chan error, 1)
	go func() {
		done <- runWatch(ctx, &buf, &errBuf, cfg, func(renderCtx context.Context) error {
			// Verify the render function receives a live context.
			assert.NoError(t, renderCtx.Err(), "context should not be cancelled during render")
			cancel()
			return nil
		})
	}()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runWatch did not stop after cancel from within renderFn")
	}
}

func TestRunWatch_MultipleRenders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	var buf, errBuf bytes.Buffer
	cfg := watchConfig{interval: 500 * time.Millisecond}

	_ = runWatch(ctx, &buf, &errBuf, cfg, func(_ context.Context) error {
		fmt.Fprint(&buf, "RENDERED")
		return nil
	})

	count := strings.Count(buf.String(), "RENDERED")
	assert.Greater(t, count, 1, "should render multiple times within the timeout")
}
