package storage

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

const (
	scrubOptReadonly        = "readonly"
	scrubOptForce           = "force"
	scrubOptIoPrioClass     = "ioprio_class"
	scrubOptIoPrioClassData = "ioprio_classdata"
)

var scrubOptsKeys = []string{scrubOptReadonly, scrubOptForce, scrubOptIoPrioClass, scrubOptIoPrioClassData}

// StartScrub starts a btrfs scrub as a background task and returns the task ID.
func (s *Storage) StartScrub(ctx context.Context, opts map[string]string, labels map[string]string, timeout time.Duration) (string, error) {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	for _, t := range s.tasks.List(string(task.TypeScrub), string(task.TypeBalance)) {
		if t.Status == task.TaskRunning || t.Status == task.TaskPending {
			return "", &StorageError{Code: ErrBusy, Message: "another maintenance task is already running"}
		}
	}
	status, err := s.btrfs.ScrubStatus(ctx, s.mountPoint)
	if err == nil && status.Running {
		return "", &StorageError{Code: ErrBusy, Message: "scrub already running on filesystem"}
	}
	if bst, _ := s.btrfs.BalanceStatus(ctx, s.mountPoint); bst != nil && bst.Running {
		return "", &StorageError{Code: ErrBusy, Message: "balance already running on filesystem"}
	}

	if err := config.ValidateLabels(labels); err != nil {
		return "", err
	}

	args, err := scrubArgsFromOpts(opts)
	if err != nil {
		return "", err
	}

	t := s.taskScrubTimeout
	if timeout > 0 {
		t = timeout
	}
	id := s.tasks.Create(string(task.TypeScrub), task.TaskOpts{Opts: opts, Labels: labels, Timeout: t}, func(ctx context.Context, update *task.Update) error {
		return s.runScrub(ctx, update, args)
	})

	log.Info().Str("task", id).Str("path", s.mountPoint).Strs("args", args).Msg("scrub started")
	return id, nil
}

func (s *Storage) runScrub(ctx context.Context, update *task.Update, args []string) error {
	stop := update.PollProgress(ctx, func() int {
		status, err := s.btrfs.ScrubStatus(ctx, s.mountPoint)
		if err != nil {
			return -1
		}
		_ = update.SetResult(status)
		scrubbed := status.DataBytesScrubbed + status.TreeBytesScrubbed
		if total := s.filesystemUsedBytes(); total > 0 {
			pct := int(scrubbed * 100 / total)
			if pct > 100 {
				return 100
			}
			return pct
		}
		return 0
	})

	err := s.btrfs.ScrubStart(ctx, s.mountPoint, args)
	stop()

	if err != nil {
		return fmt.Errorf("btrfs scrub: %w", err)
	}

	status, statusErr := s.btrfs.ScrubStatus(context.Background(), s.mountPoint)
	if statusErr != nil {
		log.Warn().Err(statusErr).Msg("failed to read scrub result")
		return nil
	}
	if err := update.SetResult(status); err != nil {
		log.Warn().Err(err).Msg("failed to store scrub result")
	}
	return nil
}

func scrubArgsFromOpts(opts map[string]string) ([]string, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	for k := range opts {
		if !slices.Contains(scrubOptsKeys, k) {
			return nil, &config.ValidationError{Message: fmt.Sprintf("unknown scrub option %q", k)}
		}
	}

	args := make([]string, 0, len(opts))
	if opts[scrubOptReadonly] == "true" {
		args = append(args, "-r")
	}
	if opts[scrubOptForce] == "true" {
		args = append(args, "-f")
	}
	if v, ok := opts[scrubOptIoPrioClass]; ok {
		n, err := config.ValidateIntInRange(v, 0, 3, scrubOptIoPrioClass)
		if err != nil {
			return nil, err
		}
		args = append(args, "-c", strconv.Itoa(n))
	}
	if v, ok := opts[scrubOptIoPrioClassData]; ok {
		n, err := config.ValidateIntInRange(v, 0, 7, scrubOptIoPrioClassData)
		if err != nil {
			return nil, err
		}
		args = append(args, "-n", strconv.Itoa(n))
	}
	return args, nil
}

func (s *Storage) filesystemUsedBytes() uint64 {
	fs := s.cachedFilesystem.Load()
	if fs == nil {
		return 0
	}
	return fs.UsedBytes
}
