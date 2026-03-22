package domain

import (
	"errors"
	"fmt"
)

// ErrMissingRequiredConfig is returned when one or more required config keys have no resolved value.
type ErrMissingRequiredConfig struct {
	Keys []string
}

func (e *ErrMissingRequiredConfig) Error() string {
	return fmt.Sprintf("missing required config keys: %v", e.Keys)
}

// ErrConfigNotOverridable is returned when the caller attempts to override a key that is
// not in the UserOverridable category.
type ErrConfigNotOverridable struct {
	Key string
}

func (e *ErrConfigNotOverridable) Error() string {
	return fmt.Sprintf("config key %q is not overridable", e.Key)
}

// ErrInvalidPortSet is returned by ExtractPortSet when a required port key is missing from
// the config values or its value cannot be parsed as an integer.
type ErrInvalidPortSet struct {
	Key   string // The env var key that is missing or malformed.
	Value string // The raw value when the problem is a parse failure (empty when the key is missing).
}

func (e *ErrInvalidPortSet) Error() string {
	if e.Value == "" {
		return fmt.Sprintf("port key %q missing from config", e.Key)
	}
	return fmt.Sprintf("port key %q has invalid value %q", e.Key, e.Value)
}

// ErrNoAvailablePort is returned by PortAllocator.AllocatePorts when all candidate ports
// are already in use.
var ErrNoAvailablePort = errors.New("no available port")
