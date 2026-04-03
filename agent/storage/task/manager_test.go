package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awaitStatus polls until a task reaches the expected status or times out.
func awaitStatus(t *testing.T, tm *Manager, id string, status TaskStatus) *Task {
	t.Helper()
	for i := 0; i < 2000; i++ {
		tsk, err := tm.Get(id)
		if err == nil && tsk.Status == status {
			return tsk
		}
		time.Sleep(5 * time.Millisecond)
	}
	tsk, _ := tm.Get(id)
	require.Failf(t, "timeout", "task %s: wanted %s, got %s", id, status, tsk.Status)
	return nil
}

// awaitDone polls until a task is completed, failed, or cancelled.
func awaitDone(t *testing.T, tm *Manager, id string) *Task {
	t.Helper()
	for i := 0; i < 2000; i++ {
		tsk, err := tm.Get(id)
		if err == nil && (tsk.Status == TaskCompleted || tsk.Status == TaskFailed || tsk.Status == TaskCancelled) {
			return tsk
		}
		time.Sleep(5 * time.Millisecond)
	}
	tsk, _ := tm.Get(id)
	require.Failf(t, "timeout", "task %s not done, status: %s", id, tsk.Status)
	return nil
}

func TestManager_SubmitAndGet(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	started := make(chan struct{})
	done := make(chan struct{})
	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		close(started)
		<-done
		return nil
	})

	<-started

	tsk, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskRunning, tsk.Status)
	assert.Equal(t, "test", tsk.Type)
	assert.NotNil(t, tsk.StartedAt)
	assert.Nil(t, tsk.CompletedAt)

	close(done)

	tsk = awaitStatus(t, tm, id, TaskCompleted)
	assert.Equal(t, 100, tsk.Progress)
	assert.NotNil(t, tsk.CompletedAt)
	assert.Empty(t, tsk.Error)
}

func TestManager_SubmitWithError(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return fmt.Errorf("something broke")
	})

	tsk := awaitStatus(t, tm, id, TaskFailed)
	assert.Equal(t, "something broke", tsk.Error)
	assert.NotNil(t, tsk.CompletedAt)
}

func TestManager_Cancel(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	started := make(chan struct{})
	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	<-started
	require.NoError(t, tm.Cancel(id))

	tsk := awaitStatus(t, tm, id, TaskCancelled)
	assert.NotNil(t, tsk.CompletedAt)
}

func TestManager_CancelFinished(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	awaitStatus(t, tm, id, TaskCompleted)
	assert.NoError(t, tm.Cancel(id))
}

func TestManager_CancelUnknown(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	err := tm.Cancel("nonexistent")
	assert.Error(t, err)
}

func TestManager_GetUnknown(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	_, err := tm.Get("nonexistent")
	assert.Error(t, err)
}

func TestManager_List(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id1 := tm.Create("scrub", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})
	id2 := tm.Create("transfer", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	awaitDone(t, tm, id1)
	awaitDone(t, tm, id2)

	all := tm.List("")
	assert.Len(t, all, 2)

	scrubs := tm.List("scrub")
	assert.Len(t, scrubs, 1)
	assert.Equal(t, "scrub", scrubs[0].Type)

	transfers := tm.List("transfer")
	assert.Len(t, transfers, 1)
	assert.Equal(t, "transfer", transfers[0].Type)

	none := tm.List("unknown")
	assert.Empty(t, none)
}

func TestManager_ListReturnsCopies(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	awaitDone(t, tm, id)

	tasks := tm.List("")
	require.Len(t, tasks, 1)

	tasks[0].Status = TaskFailed

	original, err := tm.Get(tasks[0].ID)
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, original.Status)
}

func TestManager_ProgressUpdate(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	checkpoint := make(chan struct{})
	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		update.SetProgress(50)
		close(checkpoint)
		<-ctx.Done()
		return ctx.Err()
	})

	<-checkpoint

	tsk, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, 50, tsk.Progress)

	require.NoError(t, tm.Cancel(id))
	awaitDone(t, tm, id)
}

func TestManager_ResultStruct(t *testing.T) {
	type TestResult struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}

	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return update.SetResult(TestResult{Count: 42, Name: "hello"})
	})

	tsk := awaitStatus(t, tm, id, TaskCompleted)
	require.NotNil(t, tsk.Result)

	var result TestResult
	require.NoError(t, json.Unmarshal(tsk.Result, &result))
	assert.Equal(t, 42, result.Count)
	assert.Equal(t, "hello", result.Name)
}

func TestManager_Cleanup(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	awaitDone(t, tm, id)
	tm.Cleanup(0)

	_, err := tm.Get(id)
	assert.Error(t, err)
}

func TestManager_CleanupKeepsRunning(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	done := make(chan struct{})
	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		<-done
		return nil
	})

	awaitStatus(t, tm, id, TaskRunning)
	tm.Cleanup(0)

	tsk, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskRunning, tsk.Status)

	close(done)
	awaitDone(t, tm, id)
}

func TestManager_CorruptTaskFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{corrupt"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a task"), 0o644))

	valid, _ := json.MarshalIndent(Task{ID: "good", Type: "test", Status: TaskCompleted}, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.json"), valid, 0o644))

	tm := NewManager(dir, 0, 0)

	tasks := tm.List("")
	assert.Len(t, tasks, 1)
	assert.Equal(t, "good", tasks[0].ID)
}

func TestManager_EmptyTaskFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.json"), []byte(""), 0o644))

	tm := NewManager(dir, 0, 0)
	tasks := tm.List("")
	assert.Empty(t, tasks)
}

func TestManager_ConcurrentSubmit(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	ids := make([]string, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ids[idx] = tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
				return nil
			})
		}(i)
	}
	wg.Wait()

	for _, id := range ids {
		awaitDone(t, tm, id)
	}

	tasks := tm.List("")
	assert.Len(t, tasks, 50)
	for _, tsk := range tasks {
		assert.Equal(t, TaskCompleted, tsk.Status)
	}
}

func TestManager_WorkerPoolBlocksSecondTask(t *testing.T) {
	tm := NewManager(t.TempDir(), 1, 0)

	started := make(chan struct{})
	blocker := make(chan struct{})
	first := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		close(started)
		<-blocker
		return nil
	})
	<-started

	second := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	// First running, second must be pending (pool full)
	awaitStatus(t, tm, first, TaskRunning)
	tsk, _ := tm.Get(second)
	assert.Equal(t, TaskPending, tsk.Status)

	close(blocker)

	awaitStatus(t, tm, first, TaskCompleted)
	awaitStatus(t, tm, second, TaskCompleted)
}

func TestManager_WorkerPoolMaxTwo(t *testing.T) {
	tm := NewManager(t.TempDir(), 2, 0)

	started1 := make(chan struct{})
	started2 := make(chan struct{})
	blocker := make(chan struct{})

	id1 := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		close(started1)
		<-blocker
		return nil
	})
	id2 := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		close(started2)
		<-blocker
		return nil
	})

	<-started1
	<-started2

	id3 := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	// Both slots taken, third must be pending
	tsk, _ := tm.Get(id3)
	assert.Equal(t, TaskPending, tsk.Status, "third should wait")

	close(blocker)

	awaitStatus(t, tm, id1, TaskCompleted)
	awaitStatus(t, tm, id2, TaskCompleted)
	awaitStatus(t, tm, id3, TaskCompleted)
}

func TestManager_WorkerPoolUnlimited(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	started := make(chan struct{}, 10)
	blocker := make(chan struct{})
	var ids []string
	for i := 0; i < 10; i++ {
		id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
			started <- struct{}{}
			<-blocker
			return nil
		})
		ids = append(ids, id)
	}

	// Wait for all 10 to confirm running
	for i := 0; i < 10; i++ {
		<-started
	}

	for _, id := range ids {
		tsk, _ := tm.Get(id)
		assert.Equal(t, TaskRunning, tsk.Status, "all should run with unlimited concurrency")
	}

	close(blocker)
	for _, id := range ids {
		awaitDone(t, tm, id)
	}
}

func TestManager_CancelPending(t *testing.T) {
	tm := NewManager(t.TempDir(), 1, 0)

	started := make(chan struct{})
	blocker := make(chan struct{})
	first := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		close(started)
		<-blocker
		return nil
	})
	<-started

	second := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	// Confirm pending
	tsk, _ := tm.Get(second)
	assert.Equal(t, TaskPending, tsk.Status)

	require.NoError(t, tm.Cancel(second))

	tsk = awaitStatus(t, tm, second, TaskCancelled)
	assert.NotNil(t, tsk.CompletedAt, "cancelled pending should have CompletedAt")

	close(blocker)
	awaitDone(t, tm, first)
}

func TestManager_PendingTaskRunsAfterSlotFreed(t *testing.T) {
	tm := NewManager(t.TempDir(), 1, 0)

	order := make([]string, 0, 3)
	var mu sync.Mutex

	record := func(name string) {
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}

	started := make(chan struct{})
	blocker := make(chan struct{})
	tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		record("first-start")
		close(started)
		<-blocker
		record("first-end")
		return nil
	})
	<-started

	second := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		record("second-start")
		return nil
	})

	close(blocker)
	awaitDone(t, tm, second)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, order, 3)
	assert.Equal(t, "first-start", order[0])
	assert.Equal(t, "first-end", order[1])
	assert.Equal(t, "second-start", order[2])
}

func TestManager_WorkerPoolTaskError(t *testing.T) {
	tm := NewManager(t.TempDir(), 1, 0)

	id1 := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return fmt.Errorf("boom")
	})

	awaitStatus(t, tm, id1, TaskFailed)

	id2 := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	awaitStatus(t, tm, id2, TaskCompleted)
}

func TestManager_Stress(t *testing.T) {
	tm := NewManager(t.TempDir(), 4, 0)

	const total = 1000
	var running atomic.Int32
	var maxRunning atomic.Int32

	ids := make([]string, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ids[idx] = tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
				cur := running.Add(1)
				for {
					old := maxRunning.Load()
					if cur <= old || maxRunning.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				running.Add(-1)
				return update.SetResult(map[string]string{"ok": "true"})
			})
		}(i)
	}
	wg.Wait()

	for _, id := range ids {
		awaitDone(t, tm, id)
	}

	tasks := tm.List("")
	assert.Len(t, tasks, total)

	var completed, failed int
	for _, tsk := range tasks {
		switch tsk.Status {
		case TaskCompleted:
			completed++
		case TaskFailed:
			failed++
		}
		assert.NotNil(t, tsk.CompletedAt, "task %s should have CompletedAt", tsk.ID)
		assert.NotNil(t, tsk.StartedAt, "task %s should have StartedAt", tsk.ID)
	}

	assert.Equal(t, total, completed, "all tasks should complete")
	assert.Equal(t, 0, failed, "no tasks should fail")
	assert.LessOrEqual(t, int(maxRunning.Load()), 4, "max concurrency should not exceed 4")
	t.Logf("peak concurrency: %d/4, completed: %d/%d", maxRunning.Load(), completed, total)
}

func TestManager_CreateWithOpts(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{
		Opts:    map[string]string{"sleep": "5s"},
		Timeout: 10 * time.Minute,
	}, func(ctx context.Context, update *Update) error {
		return nil
	})

	tsk := awaitStatus(t, tm, id, TaskCompleted)
	require.NotNil(t, tsk.Opts)
	assert.Equal(t, 10*time.Minute, tsk.Timeout)
	assert.Equal(t, "5s", tsk.Opts["sleep"])
}

func TestManager_Timeout(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{
		Timeout: 50 * time.Millisecond,
	}, func(ctx context.Context, update *Update) error {
		<-ctx.Done()
		return ctx.Err()
	})

	tsk := awaitStatus(t, tm, id, TaskFailed)
	assert.Contains(t, tsk.Error, "timed out")
	assert.NotNil(t, tsk.CompletedAt)
}

func TestManager_TimeoutCompletesBeforeDeadline(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{
		Timeout: 10 * time.Second,
	}, func(ctx context.Context, update *Update) error {
		return nil
	})

	tsk := awaitStatus(t, tm, id, TaskCompleted)
	assert.Empty(t, tsk.Error)
}

func TestManager_ZeroTimeoutMeansNoTimeout(t *testing.T) {
	tm := NewManager(t.TempDir(), 0, 0)

	id := tm.Create("test", TaskOpts{}, func(ctx context.Context, update *Update) error {
		return nil
	})

	tsk := awaitStatus(t, tm, id, TaskCompleted)
	assert.Equal(t, time.Duration(0), tsk.Timeout)
	assert.Nil(t, tsk.Opts)
}
