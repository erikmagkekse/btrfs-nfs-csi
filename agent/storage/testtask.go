package storage

import (
	"context"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/rs/zerolog/log"
)

// TestTaskOpts configures the test task behavior.
type TestTaskOpts struct {
	Sleep time.Duration `json:"sleep,omitempty"`
}

// StartTestTask creates a test task that sleeps for the given duration and returns "Hallo Welt".
func (s *Storage) StartTestTask(ctx context.Context, opts TestTaskOpts) (string, error) {
	id := s.tasks.Create(string(task.TypeTest), func(ctx context.Context, update *task.Update) error {
		if opts.Sleep > 0 {
			log.Debug().Dur("sleep", opts.Sleep).Msg("test task sleeping")
			select {
			case <-time.After(opts.Sleep):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return update.SetResult(map[string]string{"message": "Hallo Welt"})
	})

	log.Info().Str("task", id).Dur("sleep", opts.Sleep).Msg("test task started")
	return id, nil
}
