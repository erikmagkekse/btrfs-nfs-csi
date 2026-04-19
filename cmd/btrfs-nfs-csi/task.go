package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/urfave/cli/v3"
)

func taskResultSummary(resp *models.TaskDetailResponse) string {
	if len(resp.Result) == 0 {
		return ""
	}
	switch resp.Type {
	case models.TaskTypeScrub:
		return scrubResultSummary(resp)
	case models.TaskTypeBalance:
		return balanceResultSummary(resp)
	default:
		return genericResultSummary(resp.Result)
	}
}

func balanceResultSummary(resp *models.TaskDetailResponse) string {
	var s btrfs.BalanceStatus
	if json.Unmarshal(resp.Result, &s) != nil {
		return genericResultSummary(resp.Result)
	}
	switch resp.Status {
	case models.TaskStatusCompleted:
		if s.ChunksDone > 0 {
			return fmt.Sprintf("%d chunks balanced", s.ChunksDone)
		}
	case models.TaskStatusRunning:
		if s.ChunksTotal > 0 {
			return fmt.Sprintf("%d/%d chunks", s.ChunksDone, s.ChunksTotal)
		}
	case models.TaskStatusCancelled:
		if s.ChunksTotal > 0 {
			return fmt.Sprintf("cancelled at %d/%d chunks", s.ChunksDone, s.ChunksTotal)
		}
	case models.TaskStatusFailed:
		if s.LastError != "" {
			return s.LastError
		}
	}
	return ""
}

func scrubResultSummary(resp *models.TaskDetailResponse) string {
	var s btrfs.ScrubStatus
	if json.Unmarshal(resp.Result, &s) != nil {
		return genericResultSummary(resp.Result)
	}
	errs := s.ReadErrors + s.CSumErrors + s.VerifyErrors + s.UncorrectableErrs
	switch resp.Status {
	case models.TaskStatusRunning:
		parts := make([]string, 0, 2)
		if resp.StartedAt != nil {
			elapsed := time.Since(*resp.StartedAt).Seconds()
			if elapsed > 0 && s.DataBytesScrubbed > 0 {
				speed := float64(s.DataBytesScrubbed) / elapsed
				parts = append(parts, utils.FormatBytes(uint64(speed))+"/s")
			}
		}
		if errs > 0 {
			parts = append(parts, fmt.Sprintf("%d errors", errs))
		}
		return strings.Join(parts, ", ")
	case models.TaskStatusCompleted:
		speed := ""
		if s.DataBytesScrubbed > 0 && resp.StartedAt != nil && resp.CompletedAt != nil {
			elapsed := resp.CompletedAt.Sub(*resp.StartedAt).Seconds()
			if elapsed > 0 {
				speed = ", " + utils.FormatBytes(uint64(float64(s.DataBytesScrubbed)/elapsed)) + "/s"
			}
		}
		return fmt.Sprintf("%s scrubbed, %d errors%s", utils.FormatBytes(s.DataBytesScrubbed), errs, speed)
	case models.TaskStatusFailed:
		if errs > 0 {
			return fmt.Sprintf("%d errors", errs)
		}
	}
	return ""
}

func genericResultSummary(result json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(result, &m) != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func taskTook(createdAt time.Time, startedAt, completedAt *time.Time) string {
	if completedAt != nil {
		return completedAt.Sub(createdAt).Truncate(time.Millisecond).String()
	}
	if startedAt != nil {
		return time.Since(*startedAt).Truncate(time.Millisecond).String()
	}
	return "-"
}

func taskTimeout(t string) string {
	if t != "" {
		return t
	}
	return "-"
}

func listTasks(ctx context.Context, cmd *cli.Command, taskType, sortBy string, rev bool, opts cliListOpts) error {
	if isWide(cmd) {
		resp, err := apiClient.ListTasksDetail(ctx, taskType, opts.ListOpts, nil)
		if err != nil {
			return err
		}
		sortTasksDetail(resp.Tasks, sortBy, rev)
		return output(cmd, resp, func() {
			tw := newTableWriter(cmd, []string{"ID", "TYPE", "CREATED_BY", "STATUS", "PROGRESS", "LABELS", "TIMEOUT", "TOOK", "CREATED", "RESULT", "ERROR"})
			tw.writeHeader()
			for _, t := range resp.Tasks {
				result := taskResultSummary(&t)
				if result == "" {
					result = "-"
				}
				errMsg := "-"
				if t.Error != "" {
					errMsg = t.Error
				}
				tw.writeRow(map[string]string{
					"ID": t.ID, "TYPE": string(t.Type), "CREATED_BY": t.CreatedBy, "STATUS": string(t.Status), "PROGRESS": fmt.Sprintf("%d%%", t.Progress),
					"LABELS": formatLabelsShort(t.Labels), "TIMEOUT": taskTimeout(t.Timeout), "TOOK": taskTook(t.CreatedAt, t.StartedAt, t.CompletedAt),
					"CREATED": t.CreatedAt.Local().Format(timeFmt), "RESULT": result, "ERROR": errMsg,
				})
			}
			tw.flush()
			emptyHint("tasks", len(resp.Tasks), opts.allSet, opts.labelSet)
		})
	}
	resp, err := apiClient.ListTasks(ctx, taskType, opts.ListOpts, nil)
	if err != nil {
		return err
	}
	sortTasks(resp.Tasks, sortBy, rev)
	return output(cmd, resp, func() {
		tw := newTableWriter(cmd, []string{"ID", "TYPE", "CREATED_BY", "STATUS", "PROGRESS", "TIMEOUT", "TOOK", "CREATED"})
		tw.writeHeader()
		for _, t := range resp.Tasks {
			tw.writeRow(map[string]string{
				"ID": t.ID, "TYPE": string(t.Type), "CREATED_BY": t.CreatedBy, "STATUS": string(t.Status), "PROGRESS": fmt.Sprintf("%d%%", t.Progress),
				"TIMEOUT": taskTimeout(t.Timeout), "TOOK": taskTook(t.CreatedAt, t.StartedAt, t.CompletedAt),
				"CREATED": t.CreatedAt.Local().Format(timeFmt),
			})
		}
		tw.flush()
		emptyHint("tasks", len(resp.Tasks), opts.allSet, opts.labelSet)
	})
}

func taskGet(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return fmt.Errorf("task ID required")
	}
	resp, err := apiClient.GetTask(ctx, id)
	if err != nil {
		return wrapErr(err, "task", id)
	}
	return output(cmd, resp, func() {
		fmt.Printf("ID:         %s\n", resp.ID)
		fmt.Printf("Type:       %s\n", resp.Type)
		if len(resp.Opts) > 0 {
			parts := make([]string, 0, len(resp.Opts))
			for k, v := range resp.Opts {
				parts = append(parts, k+"="+v)
			}
			fmt.Printf("Opts:       %s\n", strings.Join(parts, ", "))
		}
		printLabels("Labels:", resp.Labels, 12)
		if resp.Timeout != "" {
			fmt.Printf("Timeout:    %s\n", resp.Timeout)
		}
		fmt.Printf("Status:     %s\n", resp.Status)
		fmt.Printf("Progress:   %d%%\n", resp.Progress)
		if resp.Error != "" {
			fmt.Printf("Error:      %s\n", resp.Error)
		}
		fmt.Printf("Created:    %s\n", resp.CreatedAt.Local().Format(timeFmt))
		if resp.StartedAt != nil {
			fmt.Printf("Started:    %s\n", resp.StartedAt.Local().Format(timeFmt))
		}
		if resp.CompletedAt != nil {
			fmt.Printf("Completed:  %s\n", resp.CompletedAt.Local().Format(timeFmt))
			fmt.Printf("Took:       %s\n", resp.CompletedAt.Sub(resp.CreatedAt).Truncate(time.Millisecond).String())
		}
		if s := taskResultSummary(resp); s != "" {
			fmt.Printf("Result:     %s\n", s)
		}
	})
}

func taskCancel(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return fmt.Errorf("task ID required")
	}
	if err := apiClient.CancelTask(ctx, id); err != nil {
		return wrapErr(err, "task", id)
	}
	if !isJSON(cmd) {
		fmt.Printf("task %q cancel requested\n", id)
	}
	return nil
}

func taskCreateScrub(ctx context.Context, cmd *cli.Command) error {
	opts := map[string]string{}
	if cmd.Bool("readonly") {
		opts["readonly"] = "true"
	}
	if cmd.Bool("force") {
		opts["force"] = "true"
	}
	if cmd.IsSet("ioprio-class") {
		opts["ioprio_class"] = fmt.Sprintf("%d", cmd.Int("ioprio-class"))
	}
	if cmd.IsSet("ioprio-classdata") {
		opts["ioprio_classdata"] = fmt.Sprintf("%d", cmd.Int("ioprio-classdata"))
	}

	req := models.TaskCreateRequest{Labels: parseLabelsFlag(cmd), Opts: opts}
	if t := cmd.Duration("timeout"); t > 0 {
		req.Timeout = t.String()
	}
	resp, err := apiClient.CreateTask(ctx, models.TaskTypeScrub, req)
	if err != nil {
		return err
	}
	if !cmd.Bool("wait") {
		return output(cmd, resp, func() {
			fmt.Printf("scrub started (task %s)\n", resp.TaskID)
		})
	}
	if !isJSON(cmd) {
		fmt.Printf("scrub started (task %s)\n", resp.TaskID)
	}
	return waitForTask(ctx, resp.TaskID)
}

func taskCreateBalance(ctx context.Context, cmd *cli.Command) error {
	opts := map[string]string{}
	for _, k := range []string{"dusage", "musage", "susage"} {
		if cmd.IsSet(k) {
			opts[k] = fmt.Sprintf("%d", cmd.Int(k))
		}
	}
	for _, k := range []string{"dprofiles", "mprofiles", "dconvert", "mconvert", "sconvert"} {
		if v := cmd.String(k); v != "" {
			opts[k] = v
		}
	}
	for _, k := range []string{"ddevid", "mdevid"} {
		if cmd.IsSet(k) {
			opts[k] = fmt.Sprintf("%d", cmd.Int(k))
		}
	}
	if cmd.Bool("force") {
		opts["force"] = "true"
	}

	req := models.TaskCreateRequest{Labels: parseLabelsFlag(cmd), Opts: opts}
	if t := cmd.Duration("timeout"); t > 0 {
		req.Timeout = t.String()
	}
	resp, err := apiClient.CreateTask(ctx, models.TaskTypeBalance, req)
	if err != nil {
		return err
	}
	if !cmd.Bool("wait") {
		return output(cmd, resp, func() {
			fmt.Printf("balance started (task %s)\n", resp.TaskID)
		})
	}
	if !isJSON(cmd) {
		fmt.Printf("balance started (task %s)\n", resp.TaskID)
	}
	return waitForTask(ctx, resp.TaskID)
}

func taskCreateDefragment(ctx context.Context, cmd *cli.Command) error {
	volume := cmd.String("volume")
	if volume == "" {
		return fmt.Errorf("defragment requires --volume")
	}

	opts := map[string]string{}
	if v := cmd.String("compress"); v != "" {
		opts["compress"] = v
	}
	if cmd.Bool("no-recursive") {
		opts["recursive"] = "false"
	}
	if cmd.IsSet("threshold") {
		opts["threshold"] = fmt.Sprintf("%d", cmd.Int("threshold"))
	}

	req := models.TaskCreateRequest{
		Labels: parseLabelsFlag(cmd),
		Opts:   opts,
		Volume: volume,
		Path:   cmd.String("path"),
	}
	if t := cmd.Duration("timeout"); t > 0 {
		req.Timeout = t.String()
	}
	resp, err := apiClient.CreateTask(ctx, models.TaskTypeDefragment, req)
	if err != nil {
		return err
	}
	if !cmd.Bool("wait") {
		return output(cmd, resp, func() {
			fmt.Printf("defragment started (task %s)\n", resp.TaskID)
		})
	}
	if !isJSON(cmd) {
		fmt.Printf("defragment started (task %s)\n", resp.TaskID)
	}
	return waitForTask(ctx, resp.TaskID)
}

func taskCreateQuotaRescan(ctx context.Context, cmd *cli.Command) error {
	req := models.TaskCreateRequest{Labels: parseLabelsFlag(cmd)}
	if t := cmd.Duration("timeout"); t > 0 {
		req.Timeout = t.String()
	}
	resp, err := apiClient.CreateTask(ctx, models.TaskTypeQuotaRescan, req)
	if err != nil {
		return err
	}
	if !cmd.Bool("wait") {
		return output(cmd, resp, func() {
			fmt.Printf("quota rescan started (task %s)\n", resp.TaskID)
		})
	}
	if !isJSON(cmd) {
		fmt.Printf("quota rescan started (task %s)\n", resp.TaskID)
	}
	return waitForTask(ctx, resp.TaskID)
}

func taskCreateTest(ctx context.Context, cmd *cli.Command) error {
	req := models.TaskCreateRequest{Labels: parseLabelsFlag(cmd)}
	if s := cmd.Duration("sleep"); s > 0 {
		req.Opts = map[string]string{"sleep": s.String()}
	}
	if t := cmd.Duration("timeout"); t > 0 {
		req.Timeout = t.String()
	}
	resp, err := apiClient.CreateTask(ctx, models.TaskTypeTest, req)
	if err != nil {
		return err
	}
	if !cmd.Bool("wait") {
		return output(cmd, resp, func() {
			fmt.Printf("test task started (task %s)\n", resp.TaskID)
		})
	}
	if !isJSON(cmd) {
		fmt.Printf("test task started (task %s)\n", resp.TaskID)
	}
	return waitForTask(ctx, resp.TaskID)
}

func waitForTask(ctx context.Context, id string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			t, err := apiClient.GetTask(ctx, id)
			if err != nil {
				return wrapErr(err, "task", id)
			}
			switch t.Status {
			case models.TaskStatusCompleted:
				took := taskTook(t.CreatedAt, t.StartedAt, t.CompletedAt)
				if s := taskResultSummary(t); s != "" {
					fmt.Printf("completed (took %s, %s)\n", took, s)
				} else {
					fmt.Printf("completed (took %s)\n", took)
				}
				return nil
			case models.TaskStatusFailed:
				return fmt.Errorf("task failed: %s", t.Error)
			case models.TaskStatusCancelled:
				fmt.Println("cancelled")
				return nil
			default:
				if s := taskResultSummary(t); s != "" {
					fmt.Printf("%s %d%% (%s)\n", t.Status, t.Progress, s)
				} else {
					fmt.Printf("%s %d%%\n", t.Status, t.Progress)
				}
			}
		}
	}
}
