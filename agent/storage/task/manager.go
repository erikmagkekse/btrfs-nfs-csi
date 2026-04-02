package task

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Manager manages async tasks as goroutines with progress tracking.
type Manager struct {
	mu      sync.Mutex
	tasks   map[string]*runningTask
	taskDir string
}

// NewManager creates a new Manager with persistence under taskDir.
func NewManager(taskDir string) *Manager {
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		log.Fatal().Err(err).Str("path", taskDir).Msg("failed to create tasks directory")
	}

	tm := &Manager{
		tasks:   make(map[string]*runningTask),
		taskDir: taskDir,
	}
	tm.loadFromDisk()
	return tm
}

// Create starts a task as a background goroutine and returns its ID.
func (tm *Manager) Create(taskType string, fn TaskFunc) string {
	id := generateID()
	now := time.Now().UTC()

	t := &Task{
		ID:        id,
		Type:      taskType,
		Status:    TaskPending,
		CreatedAt: now,
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := &runningTask{cancel: cancel}
	rt.state.Store(t)

	tm.mu.Lock()
	tm.tasks[id] = rt
	tm.mu.Unlock()

	log.Info().Str("task", id).Str("type", taskType).Msg("task submitted")

	go func() {
		start := time.Now()
		startUTC := start.UTC()

		running := *t
		running.Status = TaskRunning
		running.StartedAt = &startUTC
		rt.state.Store(&running)
		tm.persist(&running)

		err := fn(ctx, &Update{rt: rt, persist: tm.persist})

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

		elapsed := time.Since(start)
		tasksTotal.WithLabelValues(taskType, string(final.Status)).Inc()
		taskDuration.WithLabelValues(taskType).Observe(elapsed.Seconds())

		log.Info().
			Str("task", id).
			Str("type", taskType).
			Str("status", string(final.Status)).
			Dur("duration", elapsed).
			Msg("task finished")
	}()

	return id
}

// Get returns a copy of the task with the given ID.
func (tm *Manager) Get(id string) (*Task, error) {
	tm.mu.Lock()
	rt, ok := tm.tasks[id]
	tm.mu.Unlock()

	if !ok {
		return nil, ErrNotFound
	}
	cp := *rt.state.Load()
	return &cp, nil
}

// List returns copies of all tasks, optionally filtered by type.
func (tm *Manager) List(taskType string) []Task {
	tm.mu.Lock()
	snapshot := make([]*runningTask, 0, len(tm.tasks))
	for _, rt := range tm.tasks {
		snapshot = append(snapshot, rt)
	}
	tm.mu.Unlock()

	result := make([]Task, 0, len(snapshot))
	for _, rt := range snapshot {
		t := rt.state.Load()
		if taskType != "" && t.Type != taskType {
			continue
		}
		result = append(result, *t)
	}
	return result
}

// Cancel aborts a running task via context cancellation.
func (tm *Manager) Cancel(id string) error {
	tm.mu.Lock()
	rt, ok := tm.tasks[id]
	tm.mu.Unlock()

	if !ok {
		return ErrNotFound
	}
	t := rt.state.Load()
	if t.Status == TaskRunning || t.Status == TaskPending {
		if rt.cancel != nil {
			rt.cancel()
		}
		log.Info().Str("task", id).Msg("task cancelled")
	}
	return nil
}

// StartCleanup periodically removes completed/failed/cancelled tasks older than maxAge.
func (tm *Manager) StartCleanup(ctx context.Context, maxAge time.Duration) {
	go func() {
		tm.Cleanup(maxAge)
		ticker := time.NewTicker(maxAge / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tm.Cleanup(maxAge)
			}
		}
	}()
}

// Cleanup removes completed/failed/cancelled tasks older than maxAge.
func (tm *Manager) Cleanup(maxAge time.Duration) {
	cutoff := time.Now().UTC().Add(-maxAge)

	tm.mu.Lock()
	defer tm.mu.Unlock()

	var removed int
	for id, rt := range tm.tasks {
		t := rt.state.Load()
		switch t.Status {
		case TaskCompleted, TaskFailed, TaskCancelled:
			if t.CompletedAt != nil && t.CompletedAt.Before(cutoff) {
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

func (tm *Manager) persist(t *Task) {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		log.Error().Err(err).Str("task", t.ID).Msg("failed to marshal task")
		return
	}
	path := tm.taskFile(t.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Error().Err(err).Str("task", t.ID).Msg("failed to persist task")
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Error().Err(err).Str("task", t.ID).Msg("failed to persist task")
	}
}

func (tm *Manager) taskFile(id string) string {
	return filepath.Join(tm.taskDir, id+".json")
}

func (tm *Manager) loadFromDisk() {
	entries, err := os.ReadDir(tm.taskDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		var t Task
		path := filepath.Join(tm.taskDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warn().Err(err).Str("file", e.Name()).Msg("failed to load task from disk")
			continue
		}
		if err := json.Unmarshal(data, &t); err != nil {
			log.Warn().Err(err).Str("file", e.Name()).Msg("failed to load task from disk")
			continue
		}

		if t.Status == TaskRunning || t.Status == TaskPending {
			now := time.Now().UTC()
			t.Status = TaskFailed
			t.Error = "agent restarted"
			t.CompletedAt = &now
			tm.persist(&t)
		}

		rt := &runningTask{}
		rt.state.Store(&t)
		tm.tasks[t.ID] = rt

		log.Debug().Str("task", t.ID).Str("type", t.Type).Str("status", string(t.Status)).Msg("loaded task from disk")
	}
}
