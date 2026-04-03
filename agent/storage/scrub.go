package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/rs/zerolog/log"
)

// StartScrub starts a btrfs scrub as a background task and returns the task ID.
func (s *Storage) StartScrub(ctx context.Context, opts map[string]string, labels map[string]string, timeout time.Duration) (string, error) {
	for _, t := range s.tasks.List(string(task.TypeScrub)) {
		if t.Status == task.TaskRunning || t.Status == task.TaskPending {
			return "", &StorageError{Code: ErrBusy, Message: "scrub already running"}
		}
	}
	status, err := s.btrfs.ScrubStatus(ctx, s.mountPoint)
	if err == nil && status.Running {
		return "", &StorageError{Code: ErrBusy, Message: "scrub already running on filesystem"}
	}

	if err := validateLabels(labels); err != nil {
		return "", err
	}

	t := s.taskScrubTimeout
	if timeout > 0 {
		t = timeout
	}
	id := s.tasks.Create(string(task.TypeScrub), task.TaskOpts{Opts: opts, Labels: labels, Timeout: t}, func(ctx context.Context, update *task.Update) error {
		return s.runScrub(ctx, update)
	})

	log.Info().Str("task", id).Str("path", s.mountPoint).Msg("scrub started")
	return id, nil
}

func (s *Storage) runScrub(ctx context.Context, update *task.Update) error {
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

	err := s.btrfs.ScrubStart(ctx, s.mountPoint)
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

func (s *Storage) filesystemUsedBytes() uint64 {
	fs := s.cachedFilesystem.Load()
	if fs == nil {
		return 0
	}
	return fs.UsedBytes
}
