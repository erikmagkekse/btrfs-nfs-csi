package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/rs/zerolog/log"
)

const (
	scrubPollInterval = 2 * time.Second
)

// StartScrub starts a btrfs scrub as a background task and returns the task ID.
func (s *Storage) StartScrub(ctx context.Context) (string, error) {
	for _, t := range s.tasks.List(string(task.TypeScrub)) {
		if t.Status == task.TaskRunning || t.Status == task.TaskPending {
			return "", &StorageError{Code: ErrBusy, Message: "scrub already running"}
		}
	}
	status, err := s.btrfs.ScrubStatus(ctx, s.mountPoint)
	if err == nil && status.Running {
		return "", &StorageError{Code: ErrBusy, Message: "scrub already running on filesystem"}
	}

	id := s.tasks.Create(string(task.TypeScrub), func(ctx context.Context, update *task.Update) error {
		return s.runScrub(ctx, update)
	})

	log.Info().Str("task", id).Str("path", s.mountPoint).Msg("scrub started")
	return id, nil
}

func (s *Storage) runScrub(ctx context.Context, update *task.Update) error {
	stopProgress := make(chan struct{})
	go func() {
		ticker := time.NewTicker(scrubPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				status, err := s.btrfs.ScrubStatus(ctx, s.mountPoint)
				if err != nil {
					continue
				}
				scrubbed := status.DataBytesScrubbed + status.TreeBytesScrubbed
				if total := s.filesystemUsedBytes(); total > 0 {
					pct := int(scrubbed * 100 / total)
					if pct > 100 {
						pct = 100
					}
					update.SetProgress(pct)
				}
				update.SetResult(status)
			}
		}
	}()

	err := s.btrfs.ScrubStart(ctx, s.mountPoint)
	close(stopProgress)

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
