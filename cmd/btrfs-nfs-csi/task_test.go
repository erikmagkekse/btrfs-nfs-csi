package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
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

func TestScrubResultSummary_Completed(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	end := time.Now()
	resp := &models.TaskDetailResponse{
		Type:        models.TaskTypeScrub,
		Status:      models.TaskStatusCompleted,
		Result:      scrubResult(10737418240, 1048576, 0, 0),
		StartedAt:   &start,
		CompletedAt: &end,
	}
	s := scrubResultSummary(resp)
	assert.Contains(t, s, "10.0Gi scrubbed")
	assert.Contains(t, s, "0 errors")
	assert.Contains(t, s, "/s")
}

func TestScrubResultSummary_CompletedWithErrors(t *testing.T) {
	start := time.Now().Add(-5 * time.Second)
	end := time.Now()
	resp := &models.TaskDetailResponse{
		Type:        models.TaskTypeScrub,
		Status:      models.TaskStatusCompleted,
		Result:      scrubResult(1073741824, 0, 2, 1),
		StartedAt:   &start,
		CompletedAt: &end,
	}
	s := scrubResultSummary(resp)
	assert.Contains(t, s, "1.0Gi scrubbed")
	assert.Contains(t, s, "3 errors")
}

func TestScrubResultSummary_Running(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	resp := &models.TaskDetailResponse{
		Type:      models.TaskTypeScrub,
		Status:    models.TaskStatusRunning,
		Result:    scrubResult(5368709120, 0, 0, 0),
		StartedAt: &start,
	}
	s := scrubResultSummary(resp)
	assert.Contains(t, s, "/s")
}

func TestScrubResultSummary_RunningWithErrors(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	resp := &models.TaskDetailResponse{
		Type:      models.TaskTypeScrub,
		Status:    models.TaskStatusRunning,
		Result:    scrubResult(5368709120, 0, 1, 2),
		StartedAt: &start,
	}
	s := scrubResultSummary(resp)
	assert.Contains(t, s, "/s")
	assert.Contains(t, s, "3 errors")
}

func TestScrubResultSummary_Failed(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeScrub,
		Status: models.TaskStatusFailed,
		Result: scrubResult(0, 0, 5, 0),
		Error:  "scrub failed",
	}
	s := scrubResultSummary(resp)
	assert.Equal(t, "5 errors", s)
}

func TestScrubResultSummary_FailedNoErrors(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeScrub,
		Status: models.TaskStatusFailed,
		Result: scrubResult(0, 0, 0, 0),
		Error:  "scrub failed",
	}
	s := scrubResultSummary(resp)
	assert.Empty(t, s)
}

func TestTaskResultSummary_EmptyResult(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeScrub,
		Status: models.TaskStatusCompleted,
	}
	assert.Empty(t, taskResultSummary(resp))
}

func TestTaskResultSummary_UnknownType(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   "unknown",
		Status: models.TaskStatusCompleted,
		Result: json.RawMessage(`{"foo":"bar"}`),
	}
	assert.Equal(t, "foo: bar", taskResultSummary(resp))
}

func TestGenericResultSummary(t *testing.T) {
	result := json.RawMessage(`{"message":"Hallo Welt"}`)
	assert.Equal(t, "message: Hallo Welt", genericResultSummary(result))
}

func TestGenericResultSummary_MultipleKeys(t *testing.T) {
	result := json.RawMessage(`{"b":"2","a":"1"}`)
	assert.Equal(t, "a: 1, b: 2", genericResultSummary(result))
}

func TestGenericResultSummary_Invalid(t *testing.T) {
	assert.Empty(t, genericResultSummary(json.RawMessage(`{corrupt`)))
	assert.Empty(t, genericResultSummary(nil))
}

func balanceResult(done, total uint64, running, paused bool) json.RawMessage {
	s := btrfs.BalanceStatus{
		ChunksDone:  done,
		ChunksTotal: total,
		Running:     running,
		Paused:      paused,
	}
	raw, _ := json.Marshal(s)
	return raw
}

func TestBalanceResultSummary_Completed(t *testing.T) {
	// Typical case: last poll saw mid-flight state (running=true), task finished before next poll.
	// We must NOT expose the stale running flag; only surface the chunk count.
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusCompleted,
		Result: balanceResult(3, 3, true, false),
	}
	assert.Equal(t, "3 chunks balanced", balanceResultSummary(resp))
}

func TestBalanceResultSummary_CompletedNoChunks(t *testing.T) {
	// Balance finished faster than pollInterval, result has zero chunks.
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusCompleted,
		Result: balanceResult(0, 0, false, false),
	}
	assert.Empty(t, balanceResultSummary(resp))
}

func TestBalanceResultSummary_Running(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusRunning,
		Result: balanceResult(1, 3, true, false),
	}
	assert.Equal(t, "1/3 chunks", balanceResultSummary(resp))
}

func TestBalanceResultSummary_Cancelled(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusCancelled,
		Result: balanceResult(2, 10, false, false),
	}
	assert.Equal(t, "cancelled at 2/10 chunks", balanceResultSummary(resp))
}

func TestBalanceResultSummary_CancelledNoData(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusCancelled,
		Result: balanceResult(0, 0, false, false),
	}
	assert.Empty(t, balanceResultSummary(resp))
}

func TestBalanceResultSummary_Failed(t *testing.T) {
	s := btrfs.BalanceStatus{LastError: "enospc"}
	raw, _ := json.Marshal(s)
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusFailed,
		Result: raw,
	}
	assert.Equal(t, "enospc", balanceResultSummary(resp))
}

func TestTaskResultSummary_BalanceDispatch(t *testing.T) {
	resp := &models.TaskDetailResponse{
		Type:   models.TaskTypeBalance,
		Status: models.TaskStatusCompleted,
		Result: balanceResult(5, 5, false, false),
	}
	assert.Equal(t, "5 chunks balanced", taskResultSummary(resp))
}
