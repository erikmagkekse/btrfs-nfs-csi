package storage

import (
	"context"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

// StartTestTask creates a test task that sleeps for the given duration and returns "Hallo Welt".
func (s *Storage) StartTestTask(ctx context.Context, opts map[string]string, labels map[string]string, timeout time.Duration) (string, error) {
	var sleep time.Duration
	if v := opts["sleep"]; v != "" {
		var err error
		sleep, err = time.ParseDuration(v)
		if err != nil {
			return "", &StorageError{Code: ErrInvalid, Message: "invalid sleep duration: " + v}
		}
	}

	if err := config.ValidateLabels(labels); err != nil {
		return "", err
	}

	t := s.taskDefaultTimeout
	if timeout > 0 {
		t = timeout
	}
	id := s.tasks.Create(string(task.TypeTest), task.TaskOpts{Opts: opts, Labels: labels, Timeout: t}, func(ctx context.Context, update *task.Update) error {
		if sleep > 0 {
			log.Debug().Str("sleep", sleep.String()).Msg("test task sleeping")
			start := time.Now()
			stop := update.PollProgress(ctx, func() int {
				return int(time.Since(start) * 100 / sleep)
			})
			select {
			case <-time.After(sleep):
				stop()
			case <-ctx.Done():
				stop()
				return ctx.Err()
			}
		}
		return update.SetResult(map[string]string{"message": "Hallo Welt"})
	})

	log.Info().Str("task", id).Str("sleep", opts["sleep"]).Msg("test task started")
	return id, nil
}
