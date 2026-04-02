package v1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/stretchr/testify/assert"
)

func scrubResult(data, tree, readErr, csumErr uint64) json.RawMessage {
	s := btrfs.ScrubStatus{
		DataBytesScrubbed: data,
		TreeBytesScrubbed: tree,
		ReadErrors:        readErr,
		CSumErrors:        csumErr,
	}
	raw, _ := json.Marshal(s)
	return raw
}

func TestTaskInfo_Completed(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	end := time.Now()

	tk := &task.Task{
		Type:        string(task.TypeScrub),
		Status:      task.TaskCompleted,
		Result:      scrubResult(10737418240, 1048576, 0, 0),
		StartedAt:   &start,
		CompletedAt: &end,
	}

	info := taskInfo(tk)
	assert.Contains(t, info, "10.0Gi scrubbed")
	assert.Contains(t, info, "0 errors")
	assert.Contains(t, info, "/s")
}

func TestTaskInfo_CompletedWithErrors(t *testing.T) {
	start := time.Now().Add(-5 * time.Second)
	end := time.Now()

	tk := &task.Task{
		Type:        string(task.TypeScrub),
		Status:      task.TaskCompleted,
		Result:      scrubResult(1073741824, 0, 2, 1),
		StartedAt:   &start,
		CompletedAt: &end,
	}

	info := taskInfo(tk)
	assert.Contains(t, info, "1.0Gi scrubbed")
	assert.Contains(t, info, "3 errors")
}

func TestTaskInfo_Failed(t *testing.T) {
	tk := &task.Task{
		Type:   string(task.TypeScrub),
		Status: task.TaskFailed,
		Result: scrubResult(0, 0, 5, 0),
		Error:  "scrub failed",
	}

	info := taskInfo(tk)
	assert.Equal(t, "5 errors", info)
}

func TestTaskInfo_FailedNoErrors(t *testing.T) {
	tk := &task.Task{
		Type:   string(task.TypeScrub),
		Status: task.TaskFailed,
		Result: scrubResult(0, 0, 0, 0),
		Error:  "scrub failed",
	}

	info := taskInfo(tk)
	assert.Equal(t, "scrub failed", info)
}

func TestTaskInfo_EmptyResult(t *testing.T) {
	tk := &task.Task{
		Type:   string(task.TypeScrub),
		Status: task.TaskCompleted,
	}

	info := taskInfo(tk)
	assert.Equal(t, "", info)
}

func TestTaskInfo_UnknownType(t *testing.T) {
	tk := &task.Task{
		Type:   "unknown",
		Status: task.TaskCompleted,
		Result: json.RawMessage(`{"foo": "bar"}`),
	}

	info := taskInfo(tk)
	assert.Equal(t, "", info)
}

func TestTaskInfo_Running(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)

	tk := &task.Task{
		Type:      string(task.TypeScrub),
		Status:    task.TaskRunning,
		Result:    scrubResult(5368709120, 0, 0, 0),
		StartedAt: &start,
	}

	info := taskInfo(tk)
	assert.Contains(t, info, "/s")
}

func TestTaskInfo_RunningWithErrors(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)

	tk := &task.Task{
		Type:      string(task.TypeScrub),
		Status:    task.TaskRunning,
		Result:    scrubResult(5368709120, 0, 1, 2),
		StartedAt: &start,
	}

	info := taskInfo(tk)
	assert.Contains(t, info, "/s")
	assert.Contains(t, info, "3 errors")
}

func TestTaskResponseFrom(t *testing.T) {
	now := time.Now()
	tk := &task.Task{
		ID:        "abc123",
		Type:      "scrub",
		Status:    task.TaskCompleted,
		Progress:  100,
		CreatedAt: now,
	}

	resp := taskResponseFrom(tk)
	assert.Equal(t, "abc123", resp.ID)
	assert.Equal(t, "completed", resp.Status)
	assert.Equal(t, 100, resp.Progress)
}

func TestTaskDetailResponseFrom(t *testing.T) {
	now := time.Now()
	start := now.Add(-5 * time.Second)
	tk := &task.Task{
		ID:          "abc123",
		Type:        string(task.TypeScrub),
		Status:      task.TaskCompleted,
		Progress:    100,
		Result:      scrubResult(1073741824, 0, 0, 0),
		CreatedAt:   now,
		StartedAt:   &start,
		CompletedAt: &now,
	}

	resp := taskDetailResponseFrom(tk)
	assert.Equal(t, "abc123", resp.ID)
	assert.Equal(t, "completed", resp.Status)
	assert.NotEmpty(t, resp.Info)
	assert.NotNil(t, resp.Result)
}
