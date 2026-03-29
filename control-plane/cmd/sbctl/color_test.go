package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColorerStatus_ColorMapping(t *testing.T) {
	c := &colorer{enabled: true}

	tests := []struct {
		name   string
		status string
		code   string
	}{
		{"running is green", "running", "\033[32m"},
		{"creating is yellow", "creating", "\033[33m"},
		{"starting is yellow", "starting", "\033[33m"},
		{"stopping is yellow", "stopping", "\033[33m"},
		{"destroying is yellow", "destroying", "\033[33m"},
		{"stopped is dark gray", "stopped", "\033[90m"},
		{"destroyed is dark gray", "destroyed", "\033[90m"},
		{"error is red", "error", "\033[31m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.status(tt.status)
			expected := tt.code + tt.status + "\033[39m"
			assert.Equal(t, expected, got)
		})
	}
}

func TestColorerStatus_UnknownStatus(t *testing.T) {
	c := &colorer{enabled: true}
	got := c.status("unknown-status")
	expected := "\033[39m" + "unknown-status" + "\033[39m"
	assert.Equal(t, expected, got)
}

func TestColorerStatus_NoColorFlag(t *testing.T) {
	c := newColorer(0, true)
	assert.False(t, c.enabled)
	assert.Equal(t, "running", c.status("running"))
}

func TestColorerStatus_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	c := newColorer(0, false)
	assert.False(t, c.enabled)
	assert.Equal(t, "running", c.status("running"))
}

func TestColorerStatus_NoColorEnvWithValue(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	c := newColorer(0, false)
	assert.False(t, c.enabled)
	assert.Equal(t, "error", c.status("error"))
}

func TestColorerStatus_NilColorer(t *testing.T) {
	var c *colorer
	assert.Equal(t, "running", c.status("running"))
	assert.Equal(t, "error", c.status("error"))
	assert.Equal(t, "unknown", c.status("unknown"))
}

func TestColorerStatus_DisabledColorer(t *testing.T) {
	c := &colorer{enabled: false}
	assert.Equal(t, "running", c.status("running"))
	assert.Equal(t, "error", c.status("error"))
	assert.Equal(t, "stopped", c.status("stopped"))
}

func TestColorerStatus_ConsistentOverhead(t *testing.T) {
	c := &colorer{enabled: true}
	const ansiOverhead = 10 // len("\033[XXm") + len("\033[39m") = 5 + 5 = 10

	statuses := []string{
		"running", "creating", "starting", "stopping", "destroying",
		"stopped", "destroyed", "error", "something-unknown",
	}

	for _, s := range statuses {
		t.Run(s, func(t *testing.T) {
			colored := c.status(s)
			overhead := len(colored) - len(s)
			assert.Equal(t, ansiOverhead, overhead,
				"colored %q should have exactly %d bytes overhead, got %d", s, ansiOverhead, overhead)
		})
	}
}

func TestNewColorer_FlagTakesPrecedence(t *testing.T) {
	// Even if NO_COLOR is not set, --no-color flag disables colors
	t.Setenv("NO_COLOR", "")
	c := newColorer(0, true)
	assert.False(t, c.enabled)
}
