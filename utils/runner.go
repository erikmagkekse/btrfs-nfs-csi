package utils

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Runner executes shell commands and returns their combined output.
// For easy mock testing, this is abstracted behind an interface.
type Runner interface {
	Run(ctx context.Context, bin string, args ...string) (string, error)
}

// ShellRunner implements Runner using os/exec.
type ShellRunner struct{}

func (r *ShellRunner) Run(ctx context.Context, bin string, args ...string) (string, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	took := time.Since(start)

	if err != nil {
		log.Trace().Str("cmd", bin).Strs("args", args).Dur("took", took).Str("stderr", strings.TrimSpace(string(out))).Err(err).Msg("exec failed")
		return string(out), fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	log.Trace().Str("cmd", bin).Strs("args", args).Dur("took", took).Str("stdout", strings.TrimSpace(string(out))).Msg("exec ok")
	return string(out), nil
}
