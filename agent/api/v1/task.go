package v1

import (
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
)

func formatTimeout(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func taskResponseFrom(t *task.Task) TaskResponse {
	return TaskResponse{
		ID:          t.ID,
		Type:        t.Type,
		Status:      string(t.Status),
		Progress:    t.Progress,
		Opts:        t.Opts,
		Timeout:     formatTimeout(t.Timeout),
		Error:       t.Error,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
	}
}

func taskDetailResponseFrom(t *task.Task) TaskDetailResponse {
	return TaskDetailResponse{
		ID:          t.ID,
		Type:        t.Type,
		Status:      string(t.Status),
		Progress:    t.Progress,
		Opts:        t.Opts,
		Timeout:     formatTimeout(t.Timeout),
		Result:      t.Result,
		Error:       t.Error,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
	}
}
