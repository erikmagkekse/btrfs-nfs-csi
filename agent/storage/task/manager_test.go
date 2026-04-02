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

func TestManager_SubmitAndGet(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	started := make(chan struct{})
	done := make(chan struct{})
	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		close(started)
		<-done
		return nil
	})

	<-started

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskRunning, task.Status)
	assert.Equal(t, "test", task.Type)
	assert.NotNil(t, task.StartedAt)
	assert.Nil(t, task.CompletedAt)

	close(done)
	time.Sleep(50 * time.Millisecond)

	task, err = tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, task.Status)
	assert.Equal(t, 100, task.Progress)
	assert.NotNil(t, task.CompletedAt)
	assert.Empty(t, task.Error)
}

func TestManager_SubmitWithError(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		return fmt.Errorf("something broke")
	})

	time.Sleep(50 * time.Millisecond)

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskFailed, task.Status)
	assert.Equal(t, "something broke", task.Error)
	assert.NotNil(t, task.CompletedAt)
}

func TestManager_Cancel(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	started := make(chan struct{})
	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	<-started
	require.NoError(t, tm.Cancel(id))

	time.Sleep(50 * time.Millisecond)

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskCancelled, task.Status)
}

func TestManager_CancelFinished(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	err := tm.Cancel(id)
	assert.NoError(t, err)
}

func TestManager_CancelUnknown(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	err := tm.Cancel("nonexistent")
	assert.Error(t, err)
}

func TestManager_GetUnknown(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	_, err := tm.Get("nonexistent")
	assert.Error(t, err)
}

func TestManager_List(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	tm.Create("scrub", func(ctx context.Context, update *Update) error {
		return nil
	})
	tm.Create("transfer", func(ctx context.Context, update *Update) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

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
	tm := NewManager(t.TempDir(), 0)

	tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	tasks := tm.List("")
	require.Len(t, tasks, 1)

	tasks[0].Status = TaskFailed

	original, err := tm.Get(tasks[0].ID)
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, original.Status)
}

func TestManager_ProgressUpdate(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	checkpoint := make(chan struct{})
	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		update.SetProgress(50)
		close(checkpoint)
		<-ctx.Done()
		return ctx.Err()
	})

	<-checkpoint

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, 50, task.Progress)

	require.NoError(t, tm.Cancel(id))
	time.Sleep(50 * time.Millisecond)
}

func TestManager_ResultStruct(t *testing.T) {
	type TestResult struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}

	tm := NewManager(t.TempDir(), 0)

	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		return update.SetResult(TestResult{Count: 42, Name: "hello"})
	})

	time.Sleep(50 * time.Millisecond)

	task, err := tm.Get(id)
	require.NoError(t, err)
	require.NotNil(t, task.Result)

	var result TestResult
	require.NoError(t, json.Unmarshal(task.Result, &result))
	assert.Equal(t, 42, result.Count)
	assert.Equal(t, "hello", result.Name)
}

func TestManager_Cleanup(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	_, err := tm.Get(id)
	require.NoError(t, err)

	tm.Cleanup(0)

	_, err = tm.Get(id)
	assert.Error(t, err)
}

func TestManager_CleanupKeepsRunning(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	done := make(chan struct{})
	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		<-done
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	tm.Cleanup(0)

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskRunning, task.Status)

	close(done)
	time.Sleep(50 * time.Millisecond)
}

func TestManager_CorruptTaskFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{corrupt"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a task"), 0o644))

	valid, _ := json.MarshalIndent(Task{ID: "good", Type: "test", Status: TaskCompleted}, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.json"), valid, 0o644))

	tm := NewManager(dir, 0)

	tasks := tm.List("")
	assert.Len(t, tasks, 1)
	assert.Equal(t, "good", tasks[0].ID)
}

func TestManager_EmptyTaskFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.json"), []byte(""), 0o644))

	tm := NewManager(dir, 0)
	tasks := tm.List("")
	assert.Empty(t, tasks)
}

func TestManager_ConcurrentSubmit(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.Create("test", func(ctx context.Context, update *Update) error {
				return nil
			})
		}()
	}
	wg.Wait()

	time.Sleep(100 * time.Millisecond)

	tasks := tm.List("")
	assert.Len(t, tasks, 50)
	for _, task := range tasks {
		assert.Equal(t, TaskCompleted, task.Status)
	}
}

func TestManager_WorkerPoolBlocksSecondTask(t *testing.T) {
	tm := NewManager(t.TempDir(), 1)

	blocker := make(chan struct{})
	first := tm.Create("test", func(ctx context.Context, update *Update) error {
		<-blocker
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	second := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	t1, _ := tm.Get(first)
	t2, _ := tm.Get(second)
	assert.Equal(t, TaskRunning, t1.Status)
	assert.Equal(t, TaskPending, t2.Status)

	close(blocker)
	time.Sleep(100 * time.Millisecond)

	t1, _ = tm.Get(first)
	t2, _ = tm.Get(second)
	assert.Equal(t, TaskCompleted, t1.Status)
	assert.Equal(t, TaskCompleted, t2.Status)
}

func TestManager_WorkerPoolMaxTwo(t *testing.T) {
	tm := NewManager(t.TempDir(), 2)

	started1 := make(chan struct{})
	started2 := make(chan struct{})
	blocker := make(chan struct{})

	id1 := tm.Create("test", func(ctx context.Context, update *Update) error {
		close(started1)
		<-blocker
		return nil
	})
	id2 := tm.Create("test", func(ctx context.Context, update *Update) error {
		close(started2)
		<-blocker
		return nil
	})

	// Wait until both are confirmed running
	<-started1
	<-started2

	id3 := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	t1, _ := tm.Get(id1)
	t2, _ := tm.Get(id2)
	t3, _ := tm.Get(id3)
	assert.Equal(t, TaskRunning, t1.Status, "first should run")
	assert.Equal(t, TaskRunning, t2.Status, "second should run")
	assert.Equal(t, TaskPending, t3.Status, "third should wait")

	close(blocker)
	time.Sleep(100 * time.Millisecond)

	t3, _ = tm.Get(id3)
	assert.Equal(t, TaskCompleted, t3.Status, "third should complete after slots free")
}

func TestManager_WorkerPoolUnlimited(t *testing.T) {
	tm := NewManager(t.TempDir(), 0)

	blocker := make(chan struct{})
	var ids []string
	for i := 0; i < 10; i++ {
		id := tm.Create("test", func(ctx context.Context, update *Update) error {
			<-blocker
			return nil
		})
		ids = append(ids, id)
	}
	time.Sleep(50 * time.Millisecond)

	// All 10 should be running simultaneously
	for _, id := range ids {
		tsk, _ := tm.Get(id)
		assert.Equal(t, TaskRunning, tsk.Status, "all should run with unlimited concurrency")
	}

	close(blocker)
	time.Sleep(100 * time.Millisecond)
}

func TestManager_CancelPending(t *testing.T) {
	tm := NewManager(t.TempDir(), 1)

	blocker := make(chan struct{})
	tm.Create("test", func(ctx context.Context, update *Update) error {
		<-blocker
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	second := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	t2, _ := tm.Get(second)
	assert.Equal(t, TaskPending, t2.Status)

	require.NoError(t, tm.Cancel(second))
	time.Sleep(50 * time.Millisecond)

	t2, _ = tm.Get(second)
	assert.Equal(t, TaskCancelled, t2.Status)
	assert.NotNil(t, t2.CompletedAt, "cancelled pending should have CompletedAt")

	close(blocker)
	time.Sleep(50 * time.Millisecond)
}

func TestManager_PendingTaskRunsAfterSlotFreed(t *testing.T) {
	tm := NewManager(t.TempDir(), 1)

	order := make([]string, 0, 3)
	var mu sync.Mutex

	record := func(name string) {
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}

	blocker := make(chan struct{})
	tm.Create("test", func(ctx context.Context, update *Update) error {
		record("first-start")
		<-blocker
		record("first-end")
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	tm.Create("test", func(ctx context.Context, update *Update) error {
		record("second-start")
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	close(blocker)
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, order, 3)
	assert.Equal(t, "first-start", order[0])
	assert.Equal(t, "first-end", order[1])
	assert.Equal(t, "second-start", order[2])
}

func TestManager_WorkerPoolTaskError(t *testing.T) {
	tm := NewManager(t.TempDir(), 1)

	// First task fails, slot should still be freed
	id1 := tm.Create("test", func(ctx context.Context, update *Update) error {
		return fmt.Errorf("boom")
	})
	time.Sleep(50 * time.Millisecond)

	t1, _ := tm.Get(id1)
	assert.Equal(t, TaskFailed, t1.Status)

	// Second task should still run (slot freed after failure)
	id2 := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})
	time.Sleep(50 * time.Millisecond)

	t2, _ := tm.Get(id2)
	assert.Equal(t, TaskCompleted, t2.Status)
}

func TestManager_Stress(t *testing.T) {
	tm := NewManager(t.TempDir(), 4)

	const total = 1000
	var running atomic.Int32
	var maxRunning atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.Create("test", func(ctx context.Context, update *Update) error {
				cur := running.Add(1)
				// Track peak concurrency
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
		}()
	}
	wg.Wait()

	// Wait for all tasks to finish
	for i := 0; i < 300; i++ {
		tasks := tm.List("")
		done := 0
		for _, tsk := range tasks {
			if tsk.Status == TaskCompleted || tsk.Status == TaskFailed {
				done++
			}
		}
		if done == total {
			break
		}
		time.Sleep(50 * time.Millisecond)
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
