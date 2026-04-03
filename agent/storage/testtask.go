package storage

import (
	"context"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/rs/zerolog/log"
)

// TestTaskOpts configures the test task behavior.
type TestTaskOpts struct {
	Sleep string `json:"sleep,omitempty"`
}

// StartTestTask creates a test task that sleeps for the given duration and returns "Hallo Welt".
func (s *Storage) StartTestTask(ctx context.Context, opts TestTaskOpts) (string, error) {
	var sleep time.Duration
	if opts.Sleep != "" {
		var err error
		sleep, err = time.ParseDuration(opts.Sleep)
		if err != nil {
			return "", &StorageError{Code: ErrInvalid, Message: "invalid sleep duration: " + opts.Sleep}
		}
	}

	id := s.tasks.Create(string(task.TypeTest), func(ctx context.Context, update *task.Update) error {
		if sleep > 0 {
			log.Debug().Dur("sleep", sleep).Msg("test task sleeping")
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return update.SetResult(map[string]string{"message": "Hallo Welt"})
	})

	log.Info().Str("task", id).Str("sleep", opts.Sleep).Msg("test task started")
	return id, nil
}
