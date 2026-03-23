package compose

import (
	"context"
	"os/exec"
)

// cmdRunner abstracts exec.Command to allow test injection without spawning real processes.
type cmdRunner interface {
	// Run executes name with args in dir, combining stdout and stderr.
	// A non-zero exit code is returned as *exec.ExitError wrapped in the returned error.
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// osCmdRunner is the production cmdRunner backed by exec.CommandContext.
type osCmdRunner struct{}

func (r *osCmdRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return out, err
}
