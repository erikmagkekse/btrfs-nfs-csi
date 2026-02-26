package utils

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes shell commands and returns their combined output.
// For easy mock testing, this is abstracted behind an interface.
type Runner interface {
	Run(ctx context.Context, bin string, args ...string) (string, error)
}

// ShellRunner implements Runner using os/exec.
type ShellRunner struct{}

func (r *ShellRunner) Run(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
