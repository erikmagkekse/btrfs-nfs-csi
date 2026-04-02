package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
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

// Task represents an async long-running operation with progress tracking.
// Progress is kept in-memory only (updated frequently).
// Status transitions and Result are persisted to disk.
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

// TaskUpdate is passed to TaskFunc for safe progress/result updates.
type TaskUpdate struct {
	rt      *runningTask
	persist func(*Task)
}

// SetProgress atomically updates the task's progress (0-100) and persists to disk.
func (u *TaskUpdate) SetProgress(pct int) {
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
func (u *TaskUpdate) SetResult(result any) error {
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

// TaskFunc is the function executed by a task. It receives a context for
// cancellation and a TaskUpdate handle for safe progress/result updates.
type TaskFunc func(ctx context.Context, update *TaskUpdate) error

type runningTask struct {
	state  atomic.Pointer[Task]
	cancel context.CancelFunc
}

// TaskManager manages async tasks as goroutines with progress tracking.
type TaskManager struct {
	mu      sync.Mutex
	tasks   map[string]*runningTask
	taskDir string
}

// NewTaskManager creates a new TaskManager with persistence under taskDir.
func NewTaskManager(taskDir string) *TaskManager {
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		log.Fatal().Err(err).Str("path", taskDir).Msg("failed to create tasks directory")
	}

	tm := &TaskManager{
		tasks:   make(map[string]*runningTask),
		taskDir: taskDir,
	}
	tm.loadFromDisk()
	return tm
}

// Submit starts a task as a background goroutine and returns its ID.
func (tm *TaskManager) Submit(taskType string, fn TaskFunc) string {
	id := generateID()
	now := time.Now().UTC()

	task := &Task{
		ID:        id,
		Type:      taskType,
		Status:    TaskPending,
		CreatedAt: now,
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := &runningTask{cancel: cancel}
	rt.state.Store(task)

	tm.mu.Lock()
	tm.tasks[id] = rt
	tm.mu.Unlock()

	log.Info().Str("task", id).Str("type", taskType).Msg("task submitted")

	go func() {
		start := time.Now()
		startUTC := start.UTC()

		running := *task
		running.Status = TaskRunning
		running.StartedAt = &startUTC
		rt.state.Store(&running)
		tm.persist(&running) // first persist: running (skip transient pending)

		err := fn(ctx, &TaskUpdate{rt: rt, persist: tm.persist})

		now := time.Now().UTC()
		final := *rt.state.Load()
		final.CompletedAt = &now
		switch {
		case ctx.Err() != nil:
			final.Status = TaskCancelled
		case err != nil:
			final.Status = TaskFailed
			final.Error = err.Error()
		default:
			final.Status = TaskCompleted
			final.Progress = 100
		}
		rt.state.Store(&final)
		tm.persist(&final)

		log.Info().
			Str("task", id).
			Str("type", taskType).
			Str("status", string(final.Status)).
			Dur("duration", time.Since(start)).
			Msg("task finished")
	}()

	return id
}

// Get returns a copy of the task with the given ID.
func (tm *TaskManager) Get(id string) (*Task, error) {
	tm.mu.Lock()
	rt, ok := tm.tasks[id]
	tm.mu.Unlock()

	if !ok {
		return nil, &StorageError{Code: ErrNotFound, Message: "task not found"}
	}
	cp := *rt.state.Load()
	return &cp, nil
}

// List returns copies of all tasks, optionally filtered by type.
func (tm *TaskManager) List(taskType string) []Task {
	tm.mu.Lock()
	snapshot := make([]*runningTask, 0, len(tm.tasks))
	for _, rt := range tm.tasks {
		snapshot = append(snapshot, rt)
	}
	tm.mu.Unlock()

	result := make([]Task, 0, len(snapshot))
	for _, rt := range snapshot {
		task := rt.state.Load()
		if taskType != "" && task.Type != taskType {
			continue
		}
		result = append(result, *task)
	}
	return result
}

// Cancel aborts a running task via context cancellation.
func (tm *TaskManager) Cancel(id string) error {
	tm.mu.Lock()
	rt, ok := tm.tasks[id]
	tm.mu.Unlock()

	if !ok {
		return &StorageError{Code: ErrNotFound, Message: "task not found"}
	}
	task := rt.state.Load()
	if task.Status == TaskRunning || task.Status == TaskPending {
		if rt.cancel != nil {
			rt.cancel()
		}
		log.Info().Str("task", id).Msg("task cancelled")
	}
	return nil
}

// StartCleanup periodically removes completed/failed/cancelled tasks
// older than maxAge.
func (tm *TaskManager) StartCleanup(ctx context.Context, maxAge time.Duration) {
	go func() {
		tm.cleanup(maxAge)
		ticker := time.NewTicker(maxAge / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tm.cleanup(maxAge)
			}
		}
	}()
}

func (tm *TaskManager) cleanup(maxAge time.Duration) {
	cutoff := time.Now().UTC().Add(-maxAge)

	tm.mu.Lock()
	defer tm.mu.Unlock()

	var removed int
	for id, rt := range tm.tasks {
		task := rt.state.Load()
		switch task.Status {
		case TaskCompleted, TaskFailed, TaskCancelled:
			if task.CompletedAt != nil && task.CompletedAt.Before(cutoff) {
				delete(tm.tasks, id)
				_ = os.Remove(tm.taskFile(id))
				removed++
			}
		}
	}
	if removed > 0 {
		log.Info().Int("removed", removed).Msg("task cleanup complete")
	}
}

func (tm *TaskManager) persist(task *Task) {
	path := tm.taskFile(task.ID)
	if err := writeMetadataAtomic(path, task); err != nil {
		log.Error().Err(err).Str("task", task.ID).Msg("failed to persist task")
	}
}

func (tm *TaskManager) taskFile(id string) string {
	return filepath.Join(tm.taskDir, id+".json")
}

// loadFromDisk reads persisted tasks on startup. Tasks that were running
// when the agent died are marked as failed.
func (tm *TaskManager) loadFromDisk() {
	entries, err := os.ReadDir(tm.taskDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		var task Task
		path := filepath.Join(tm.taskDir, e.Name())
		if err := ReadMetadata(path, &task); err != nil {
			log.Warn().Err(err).Str("file", e.Name()).Msg("failed to load task from disk")
			continue
		}

		if task.Status == TaskRunning || task.Status == TaskPending {
			now := time.Now().UTC()
			task.Status = TaskFailed
			task.Error = "agent restarted"
			task.CompletedAt = &now
			if err := writeMetadataAtomic(path, &task); err != nil {
				log.Warn().Err(err).Str("task", task.ID).Msg("failed to update stale task")
			}
		}

		rt := &runningTask{}
		rt.state.Store(&task)
		tm.tasks[task.ID] = rt

		log.Debug().Str("task", task.ID).Str("type", task.Type).Str("status", string(task.Status)).Msg("loaded task from disk")
	}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
