package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// TaskType identifies the kind of background operation.
type TaskType string

const (
	TypeScrub TaskType = "scrub"
	TypeTest  TaskType = "test"
)

// ErrNotFound is returned when a task ID doesn't exist.
var ErrNotFound = fmt.Errorf("task not found")

// Task represents an async long-running operation with progress tracking.
type Task struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Status      TaskStatus        `json:"status"`
	Progress    int               `json:"progress"`
	Opts        map[string]string `json:"opts,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	Result      json.RawMessage   `json:"result,omitempty"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
}

func (t Task) GetLabels() map[string]string { return t.Labels }

// TaskOpts configures a new task.
type TaskOpts struct {
	Opts    map[string]string
	Labels  map[string]string
	Timeout time.Duration
}

// TaskFunc is the function executed by a task.
type TaskFunc func(ctx context.Context, update *Update) error

// Update is passed to TaskFunc for safe progress/result updates.
type Update struct {
	rt           *runningTask
	persist      func(*Task)
	pollInterval time.Duration
}

// PollProgress runs fn every pollInterval until stopped or ctx is cancelled.
// fn returns the current progress (0-100). Negative values are ignored (skip update).
// Returns a stop function that must be called when polling is no longer needed.
// The stop function is safe to call multiple times.
func (u *Update) PollProgress(ctx context.Context, fn func() int) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(u.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if pct := fn(); pct >= 0 {
					u.SetProgress(pct)
				}
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// SetProgress atomically updates the task's progress (0-100) and persists to disk.
func (u *Update) SetProgress(pct int) {
	for {
		old := u.rt.state.Load()
		if old.Progress == pct {
			return
		}
		cp := *old
		cp.Progress = pct
		if u.rt.state.CompareAndSwap(old, &cp) {
			u.persist(&cp)
			return
		}
	}
}

// SetResult atomically updates the task's result.
func (u *Update) SetResult(result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	for {
		old := u.rt.state.Load()
		cp := *old
		cp.Result = raw
		if u.rt.state.CompareAndSwap(old, &cp) {
			return nil
		}
	}
}

type runningTask struct {
	state  atomic.Pointer[Task]
	cancel context.CancelFunc
}
