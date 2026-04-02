package task

import (
	"context"
	"encoding/json"
	"fmt"
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
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Status      TaskStatus      `json:"status"`
	Progress    int             `json:"progress"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// TaskFunc is the function executed by a task.
type TaskFunc func(ctx context.Context, update *Update) error

// Update is passed to TaskFunc for safe progress/result updates.
type Update struct {
	rt      *runningTask
	persist func(*Task)
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
