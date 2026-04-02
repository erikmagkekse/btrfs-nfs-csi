package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_SubmitAndGet(t *testing.T) {
	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

	id := tm.Create("test", func(ctx context.Context, update *Update) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	err := tm.Cancel(id)
	assert.NoError(t, err)
}

func TestManager_CancelUnknown(t *testing.T) {
	tm := NewManager(t.TempDir())

	err := tm.Cancel("nonexistent")
	assert.Error(t, err)
}

func TestManager_GetUnknown(t *testing.T) {
	tm := NewManager(t.TempDir())

	_, err := tm.Get("nonexistent")
	assert.Error(t, err)
}

func TestManager_List(t *testing.T) {
	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

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

	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

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
	tm := NewManager(t.TempDir())

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

	tm := NewManager(dir)

	tasks := tm.List("")
	assert.Len(t, tasks, 1)
	assert.Equal(t, "good", tasks[0].ID)
}

func TestManager_EmptyTaskFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.json"), []byte(""), 0o644))

	tm := NewManager(dir)
	tasks := tm.List("")
	assert.Empty(t, tasks)
}

func TestManager_ConcurrentSubmit(t *testing.T) {
	tm := NewManager(t.TempDir())

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
