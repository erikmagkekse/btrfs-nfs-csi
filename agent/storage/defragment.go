package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

const (
	defragmentOptCompress  = "compress"
	defragmentOptRecursive = "recursive"
	defragmentOptThreshold = "threshold"
)

var (
	defragmentOptsKeys    = []string{defragmentOptCompress, defragmentOptRecursive, defragmentOptThreshold}
	validCompressionAlgos = []string{"zstd", "lzo", "zlib", "none"}
)

// StartDefragment starts a btrfs defragment as a background task against a
// single volume, optionally scoped to a sub-path relative to the volume's
// data dir. Snapshots are not supported because the agent creates them
// read-only; use a clone instead.
func (s *Storage) StartDefragment(ctx context.Context, tenant, volume, relPath string, opts map[string]string, labels map[string]string, timeout time.Duration) (string, error) {
	if volume == "" {
		return "", &config.ValidationError{Message: "defragment requires a volume"}
	}
	if err := config.ValidateName(volume); err != nil {
		return "", err
	}
	baseDataDir, err := s.volumes.DataPath(tenant, volume)
	if err != nil {
		return "", &config.ValidationError{Message: err.Error()}
	}

	target, err := resolveDefragTarget(baseDataDir, relPath)
	if err != nil {
		return "", err
	}

	if err := config.ValidateLabels(labels); err != nil {
		return "", err
	}

	args, err := defragmentArgsFromOpts(opts)
	if err != nil {
		return "", err
	}

	t := s.taskDefaultTimeout
	if timeout > 0 {
		t = timeout
	}
	id := s.tasks.Create(string(task.TypeDefragment), task.TaskOpts{Opts: opts, Labels: labels, Timeout: t}, func(ctx context.Context, update *task.Update) error {
		return s.runDefragment(ctx, update, target, args)
	})

	log.Info().Str("task", id).Str("target", target).Strs("args", args).Msg("defragment started")
	return id, nil
}

func (s *Storage) runDefragment(ctx context.Context, update *task.Update, target string, args []string) error {
	// btrfs defragment has no native progress reporting; we set a coarse
	// midpoint so clients can distinguish "still running" from "not yet
	// started". The framework takes the task to 100 on success.
	update.SetProgress(50)
	if err := s.btrfs.Defragment(ctx, target, args); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("btrfs defragment: %w", err)
	}
	return nil
}

// resolveDefragTarget joins baseDataDir with relPath, enforcing that the
// result stays under baseDataDir after symlink resolution. An empty relPath
// returns baseDataDir; the base itself must still exist.
func resolveDefragTarget(baseDataDir, relPath string) (string, error) {
	if relPath == "" {
		if _, err := os.Stat(baseDataDir); err != nil {
			if os.IsNotExist(err) {
				return "", &config.ValidationError{Message: "defragment target volume not found"}
			}
			return "", &config.ValidationError{Message: fmt.Sprintf("defragment target: %s", err)}
		}
		return baseDataDir, nil
	}
	if filepath.IsAbs(relPath) {
		return "", &config.ValidationError{Message: "defragment path must be relative"}
	}
	cleaned := filepath.Clean(relPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", &config.ValidationError{Message: fmt.Sprintf("defragment path %q escapes volume directory", relPath)}
	}
	joined := filepath.Join(baseDataDir, cleaned)
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &config.ValidationError{Message: fmt.Sprintf("defragment path %q does not exist", relPath)}
		}
		return "", &config.ValidationError{Message: fmt.Sprintf("defragment path %q: %s", relPath, err)}
	}
	if real != baseDataDir && !strings.HasPrefix(real, baseDataDir+string(filepath.Separator)) {
		return "", &config.ValidationError{Message: fmt.Sprintf("defragment path %q escapes volume directory via symlink", relPath)}
	}
	return joined, nil
}

func defragmentArgsFromOpts(opts map[string]string) ([]string, error) {
	for k := range opts {
		if !slices.Contains(defragmentOptsKeys, k) {
			return nil, &config.ValidationError{Message: fmt.Sprintf("unknown defragment option %q", k)}
		}
	}

	args := make([]string, 0, len(opts))

	// Recursive defaults to true; "false" disables.
	if opts[defragmentOptRecursive] != "false" {
		args = append(args, "-r")
	}

	if v, ok := opts[defragmentOptCompress]; ok && v != "" && v != "none" {
		if !slices.Contains(validCompressionAlgos, v) {
			return nil, &config.ValidationError{Message: fmt.Sprintf("invalid compress value %q (expected one of: zstd, lzo, zlib, none)", v)}
		}
		args = append(args, "-c"+v)
	}

	if v, ok := opts[defragmentOptThreshold]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, &config.ValidationError{Message: fmt.Sprintf("invalid threshold value %q (expected positive integer bytes)", v)}
		}
		args = append(args, "-t", strconv.Itoa(n))
	}

	return args, nil
}
