package v1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
)

func taskResponseFrom(t *task.Task) TaskResponse {
	return TaskResponse{
		ID:          t.ID,
		Type:        t.Type,
		Status:      string(t.Status),
		Progress:    t.Progress,
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
		Result:      t.Result,
		Info:        taskInfo(t),
		Error:       t.Error,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
	}
}

func taskInfo(t *task.Task) string {
	if len(t.Result) == 0 {
		return ""
	}
	switch t.Type {
	case string(task.TypeScrub):
		var s btrfs.ScrubStatus
		if json.Unmarshal(t.Result, &s) != nil {
			return ""
		}
		errs := s.ReadErrors + s.CSumErrors + s.VerifyErrors + s.UncorrectableErrs
		switch t.Status {
		case task.TaskRunning:
			parts := make([]string, 0, 2)
			if t.StartedAt != nil {
				elapsed := float64(int(time.Since(*t.StartedAt).Seconds()))
				if elapsed > 0 && s.DataBytesScrubbed > 0 {
					speed := float64(s.DataBytesScrubbed) / elapsed
					parts = append(parts, utils.FormatBytes(uint64(speed))+"/s")
				}
			}
			if errs > 0 {
				parts = append(parts, fmt.Sprintf("%d errors", errs))
			}
			if len(parts) == 0 {
				return ""
			}
			return strings.Join(parts, ", ")
		case task.TaskCompleted:
			speed := ""
			if s.DataBytesScrubbed > 0 && t.StartedAt != nil && t.CompletedAt != nil {
				elapsed := t.CompletedAt.Sub(*t.StartedAt).Seconds()
				if elapsed > 0 {
					speed = ", " + utils.FormatBytes(uint64(float64(s.DataBytesScrubbed)/elapsed)) + "/s"
				}
			}
			return fmt.Sprintf("%s scrubbed, %d errors%s", utils.FormatBytes(s.DataBytesScrubbed), errs, speed)
		case task.TaskFailed:
			if errs > 0 {
				return fmt.Sprintf("%d errors", errs)
			}
			return t.Error
		default:
			return ""
		}
	default:
		return ""
	}
}
