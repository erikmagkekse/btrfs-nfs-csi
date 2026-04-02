package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskManager_SubmitAndGet(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	started := make(chan struct{})
	done := make(chan struct{})
	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
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

func TestTaskManager_SubmitWithError(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
		return fmt.Errorf("something broke")
	})

	time.Sleep(50 * time.Millisecond)

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskFailed, task.Status)
	assert.Equal(t, "something broke", task.Error)
	assert.NotNil(t, task.CompletedAt)
}

func TestTaskManager_Cancel(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	started := make(chan struct{})
	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
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

func TestTaskManager_CancelFinished(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	err := tm.Cancel(id)
	assert.NoError(t, err)
}

func TestTaskManager_CancelUnknown(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	err := tm.Cancel("nonexistent")
	requireStorageError(t, err, ErrNotFound)
}

func TestTaskManager_GetUnknown(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	_, err := tm.Get("nonexistent")
	requireStorageError(t, err, ErrNotFound)
}

func TestTaskManager_List(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	tm.Submit(TaskTypeScrub, func(ctx context.Context, update *TaskUpdate) error {
		return nil
	})
	tm.Submit("transfer", func(ctx context.Context, update *TaskUpdate) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	all := tm.List("")
	assert.Len(t, all, 2)

	scrubs := tm.List(TaskTypeScrub)
	assert.Len(t, scrubs, 1)
	assert.Equal(t, TaskTypeScrub, scrubs[0].Type)

	transfers := tm.List("transfer")
	assert.Len(t, transfers, 1)
	assert.Equal(t, "transfer", transfers[0].Type)

	none := tm.List("unknown")
	assert.Empty(t, none)
}

func TestTaskManager_ListReturnsCopies(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
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

func TestTaskManager_ProgressUpdate(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	checkpoint := make(chan struct{})
	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
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

func TestTaskManager_ResultStruct(t *testing.T) {
	type TestResult struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}

	tm := NewTaskManager(t.TempDir())

	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
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

func TestTaskManager_Cleanup(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	_, err := tm.Get(id)
	require.NoError(t, err)

	tm.cleanup(0)

	_, err = tm.Get(id)
	requireStorageError(t, err, ErrNotFound)
}

func TestTaskManager_CleanupKeepsRunning(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	done := make(chan struct{})
	id := tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
		<-done
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	tm.cleanup(0)

	task, err := tm.Get(id)
	require.NoError(t, err)
	assert.Equal(t, TaskRunning, task.Status)

	close(done)
	time.Sleep(50 * time.Millisecond)
}

func TestTaskManager_ConcurrentSubmit(t *testing.T) {
	tm := NewTaskManager(t.TempDir())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.Submit("test", func(ctx context.Context, update *TaskUpdate) error {
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
