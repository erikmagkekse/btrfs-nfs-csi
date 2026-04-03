package ctl

import (
	"encoding/json"
	"testing"
	"time"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
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
	resp := &v1.TaskDetailResponse{
		Type:        v1.TaskTypeScrub,
		Status:      v1.TaskStatusCompleted,
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
	resp := &v1.TaskDetailResponse{
		Type:        v1.TaskTypeScrub,
		Status:      v1.TaskStatusCompleted,
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
	resp := &v1.TaskDetailResponse{
		Type:      v1.TaskTypeScrub,
		Status:    v1.TaskStatusRunning,
		Result:    scrubResult(5368709120, 0, 0, 0),
		StartedAt: &start,
	}
	s := scrubResultSummary(resp)
	assert.Contains(t, s, "/s")
}

func TestScrubResultSummary_RunningWithErrors(t *testing.T) {
	start := time.Now().Add(-10 * time.Second)
	resp := &v1.TaskDetailResponse{
		Type:      v1.TaskTypeScrub,
		Status:    v1.TaskStatusRunning,
		Result:    scrubResult(5368709120, 0, 1, 2),
		StartedAt: &start,
	}
	s := scrubResultSummary(resp)
	assert.Contains(t, s, "/s")
	assert.Contains(t, s, "3 errors")
}

func TestScrubResultSummary_Failed(t *testing.T) {
	resp := &v1.TaskDetailResponse{
		Type:   v1.TaskTypeScrub,
		Status: v1.TaskStatusFailed,
		Result: scrubResult(0, 0, 5, 0),
		Error:  "scrub failed",
	}
	s := scrubResultSummary(resp)
	assert.Equal(t, "5 errors", s)
}

func TestScrubResultSummary_FailedNoErrors(t *testing.T) {
	resp := &v1.TaskDetailResponse{
		Type:   v1.TaskTypeScrub,
		Status: v1.TaskStatusFailed,
		Result: scrubResult(0, 0, 0, 0),
		Error:  "scrub failed",
	}
	s := scrubResultSummary(resp)
	assert.Empty(t, s)
}

func TestTaskResultSummary_EmptyResult(t *testing.T) {
	resp := &v1.TaskDetailResponse{
		Type:   v1.TaskTypeScrub,
		Status: v1.TaskStatusCompleted,
	}
	assert.Empty(t, taskResultSummary(resp))
}

func TestTaskResultSummary_UnknownType(t *testing.T) {
	resp := &v1.TaskDetailResponse{
		Type:   "unknown",
		Status: v1.TaskStatusCompleted,
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
