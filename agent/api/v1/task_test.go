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

func TestTaskResponseFrom(t *testing.T) {
	now := time.Now()
	tk := &task.Task{
		ID:        "abc123",
		Type:      "scrub",
		Status:    task.TaskCompleted,
		Progress:  100,
		Opts:      map[string]string{"sleep": "5s"},
		Timeout:   24 * time.Hour,
		CreatedAt: now,
	}

	resp := taskResponseFrom(tk)
	assert.Equal(t, "abc123", resp.ID)
	assert.Equal(t, "completed", resp.Status)
	assert.Equal(t, 100, resp.Progress)
	assert.Equal(t, map[string]string{"sleep": "5s"}, resp.Opts)
	assert.Equal(t, "24h0m0s", resp.Timeout)
}

func TestTaskResponseFrom_NoOptsNoTimeout(t *testing.T) {
	now := time.Now()
	tk := &task.Task{
		ID:        "abc123",
		Type:      "scrub",
		Status:    task.TaskCompleted,
		Progress:  100,
		CreatedAt: now,
	}

	resp := taskResponseFrom(tk)
	assert.Nil(t, resp.Opts)
	assert.Empty(t, resp.Timeout)
}

func TestTaskDetailResponseFrom(t *testing.T) {
	now := time.Now()
	start := now.Add(-5 * time.Second)
	tk := &task.Task{
		ID:          "abc123",
		Type:        string(task.TypeScrub),
		Status:      task.TaskCompleted,
		Progress:    100,
		Opts:        map[string]string{"key": "val"},
		Timeout:     6 * time.Hour,
		Result:      scrubResult(1073741824, 0, 0, 0),
		CreatedAt:   now,
		StartedAt:   &start,
		CompletedAt: &now,
	}

	resp := taskDetailResponseFrom(tk)
	assert.Equal(t, "abc123", resp.ID)
	assert.Equal(t, "completed", resp.Status)
	assert.NotNil(t, resp.Result)
	assert.Equal(t, map[string]string{"key": "val"}, resp.Opts)
	assert.Equal(t, "6h0m0s", resp.Timeout)
}

func TestTaskDetailResponseFrom_WithLabels(t *testing.T) {
	labels := map[string]string{"created-by": "cli", "env": "prod"}
	tk := &task.Task{
		ID:        "abc",
		Type:      "test",
		Status:    task.TaskCompleted,
		Labels:    labels,
		CreatedAt: time.Now(),
	}

	detail := taskDetailResponseFrom(tk)
	assert.Equal(t, labels, detail.Labels)

	summary := taskResponseFrom(tk)
	assert.Nil(t, summary.Opts)
}
