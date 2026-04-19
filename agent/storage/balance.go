package storage

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

var balanceOptsKeys = []string{
	"dusage", "musage", "susage",
	"dprofiles", "mprofiles",
	"ddevid", "mdevid",
	"dconvert", "mconvert", "sconvert",
	"force",
}

func (s *Storage) StartBalance(ctx context.Context, opts map[string]string, labels map[string]string, timeout time.Duration) (string, error) {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	for _, t := range s.tasks.List(string(task.TypeScrub), string(task.TypeBalance)) {
		if t.Status == task.TaskRunning || t.Status == task.TaskPending {
			return "", &StorageError{Code: ErrBusy, Message: "another maintenance task is already running"}
		}
	}
	if bst, _ := s.btrfs.BalanceStatus(ctx, s.mountPoint); bst != nil && bst.Running {
		return "", &StorageError{Code: ErrBusy, Message: "balance already running on filesystem"}
	}
	if sst, err := s.btrfs.ScrubStatus(ctx, s.mountPoint); err == nil && sst.Running {
		return "", &StorageError{Code: ErrBusy, Message: "scrub already running on filesystem"}
	}

	if err := config.ValidateLabels(labels); err != nil {
		return "", err
	}

	args, err := balanceArgsFromOpts(opts)
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		log.Warn().Str("path", s.mountPoint).Msg("balance without filters: rewriting entire filesystem, expect heavy IO")
	}

	t := s.taskBalanceTimeout
	if timeout > 0 {
		t = timeout
	}
	id := s.tasks.Create(string(task.TypeBalance), task.TaskOpts{Opts: opts, Labels: labels, Timeout: t}, func(ctx context.Context, update *task.Update) error {
		return s.runBalance(ctx, update, args)
	})

	log.Info().Str("task", id).Str("path", s.mountPoint).Strs("args", args).Msg("balance started")
	return id, nil
}

func (s *Storage) runBalance(ctx context.Context, update *task.Update, args []string) error {
	var lastStatus atomic.Pointer[btrfs.BalanceStatus]

	stop := update.PollProgress(ctx, func() int {
		status, err := s.btrfs.BalanceStatus(ctx, s.mountPoint)
		if err != nil || status == nil {
			return -1
		}
		lastStatus.Store(status)
		_ = update.SetResult(status)
		if status.ChunksTotal > 0 {
			pct := int(status.ChunksDone * 100 / status.ChunksTotal)
			if pct > 100 {
				return 100
			}
			return pct
		}
		return 0
	})

	// Unlike scrub, killing the foreground process does not stop the kernel-side
	// balance. The watcher fires `btrfs balance cancel` on ctx cancellation, but
	// must NOT fire on normal completion, hence the completed channel.
	completed := make(chan struct{})
	cancelDone := make(chan struct{})
	go func() {
		defer close(cancelDone)
		select {
		case <-completed:
			return
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.btrfs.BalanceCancel(cancelCtx, s.mountPoint); err != nil {
				log.Warn().Err(err).Str("path", s.mountPoint).Msg("btrfs balance cancel failed; kernel balance may continue")
			}
		}
	}()

	err := s.btrfs.BalanceStart(ctx, s.mountPoint, args)
	close(completed)
	stop()
	<-cancelDone

	if final := finalizeBalanceResult(lastStatus.Load(), err, ctx.Err()); final != nil {
		_ = update.SetResult(final)
	}

	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("btrfs balance: %w", err)
	}
	return nil
}

// finalizeBalanceResult sanitizes the last poll snapshot so the persisted Result
// matches the terminal task state. The last poll may have frozen a mid-flight
// snapshot (Running=true, ChunksDone<ChunksTotal); after the kernel-side balance
// has stopped, Running and Paused must be false. On successful completion, every
// considered chunk was processed, so Done is aligned with Total.
func finalizeBalanceResult(last *btrfs.BalanceStatus, balanceErr, ctxErr error) *btrfs.BalanceStatus {
	if last == nil {
		return nil
	}
	final := *last
	final.Running = false
	final.Paused = false
	if balanceErr == nil && ctxErr == nil && final.ChunksTotal > 0 {
		final.ChunksDone = final.ChunksTotal
	}
	return &final
}

func balanceArgsFromOpts(opts map[string]string) ([]string, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	for k := range opts {
		if !slices.Contains(balanceOptsKeys, k) {
			return nil, &config.ValidationError{Message: fmt.Sprintf("unknown balance option %q", k)}
		}
	}

	args := make([]string, 0, len(opts))

	for _, k := range []string{"dusage", "musage", "susage"} {
		if v, ok := opts[k]; ok {
			pct, err := config.ValidateIntInRange(v, 0, 100, k)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s=%d", k, pct))
		}
	}

	for _, k := range []string{"dprofiles", "mprofiles"} {
		if v, ok := opts[k]; ok {
			if !slices.Contains(btrfs.ValidProfiles, v) {
				return nil, &config.ValidationError{Message: fmt.Sprintf("invalid %s value %q", k, v)}
			}
			args = append(args, fmt.Sprintf("-%s=%s", k, v))
		}
	}

	for _, k := range []string{"ddevid", "mdevid"} {
		if v, ok := opts[k]; ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return nil, &config.ValidationError{Message: fmt.Sprintf("invalid %s value %q (expected positive integer)", k, v)}
			}
			args = append(args, fmt.Sprintf("-%s=%d", k, n))
		}
	}

	for _, k := range []string{"dconvert", "mconvert", "sconvert"} {
		if v, ok := opts[k]; ok {
			profile, _ := strings.CutSuffix(v, ",soft")
			if !slices.Contains(btrfs.ValidProfiles, profile) {
				return nil, &config.ValidationError{Message: fmt.Sprintf("invalid %s profile %q", k, v)}
			}
			args = append(args, fmt.Sprintf("-%s=%s", k, v))
		}
	}

	if v, ok := opts["force"]; ok && v == "true" {
		args = append(args, "-f")
	}

	return args, nil
}
