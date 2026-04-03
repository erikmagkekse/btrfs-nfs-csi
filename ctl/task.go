package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/urfave/cli/v3"
)

func taskResultSummary(resp *v1.TaskDetailResponse) string {
	if len(resp.Result) == 0 {
		return ""
	}
	switch resp.Type {
	case v1.TaskTypeScrub:
		return scrubResultSummary(resp)
	default:
		return genericResultSummary(resp.Result)
	}
}

func scrubResultSummary(resp *v1.TaskDetailResponse) string {
	var s btrfs.ScrubStatus
	if json.Unmarshal(resp.Result, &s) != nil {
		return genericResultSummary(resp.Result)
	}
	errs := s.ReadErrors + s.CSumErrors + s.VerifyErrors + s.UncorrectableErrs
	switch resp.Status {
	case v1.TaskStatusRunning:
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
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, ", ")
	case v1.TaskStatusCompleted:
		speed := ""
		if s.DataBytesScrubbed > 0 && resp.StartedAt != nil && resp.CompletedAt != nil {
			elapsed := resp.CompletedAt.Sub(*resp.StartedAt).Seconds()
			if elapsed > 0 {
				speed = ", " + utils.FormatBytes(uint64(float64(s.DataBytesScrubbed)/elapsed)) + "/s"
			}
		}
		return fmt.Sprintf("%s scrubbed, %d errors%s", utils.FormatBytes(s.DataBytesScrubbed), errs, speed)
	case v1.TaskStatusFailed:
		if errs > 0 {
			return fmt.Sprintf("%d errors", errs)
		}
		return ""
	default:
		return ""
	}
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
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func taskCmd() *cli.Command {
	return &cli.Command{
		Name:    "task",
		Aliases: []string{"tasks"},
		Usage:   "manage background tasks",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list tasks",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "filter by type (e.g. scrub)"},
					labelFlag(),
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					taskType := cmd.String("type")
					opts := v1.ListOpts{Labels: cmd.StringSlice("label")}
					if isWide(cmd) {
						resp, err := c.ListTasksDetail(ctx, taskType, opts)
						if err != nil {
							return err
						}
						sort.Slice(resp.Tasks, func(i, j int) bool {
							return resp.Tasks[i].CreatedAt.After(resp.Tasks[j].CreatedAt)
						})
						return output(cmd, resp, func() {
							w := tab()
							_, _ = fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tPROGRESS\tLABELS\tTIMEOUT\tTOOK\tCREATED\tRESULT\tERROR")
							for _, t := range resp.Tasks {
								took := "-"
								if t.CompletedAt != nil {
									took = fmtDuration(t.CompletedAt.Sub(t.CreatedAt))
								} else if t.StartedAt != nil {
									took = fmtDuration(time.Since(*t.StartedAt))
								}
								result := taskResultSummary(&t)
								if result == "" {
									result = "-"
								}
								timeout := "-"
								if t.Timeout != "" {
									timeout = t.Timeout
								}
								errMsg := "-"
								if t.Error != "" {
									errMsg = t.Error
								}
								_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d%%\t%s\t%s\t%s\t%s\t%s\t%s\n",
									t.ID, t.Type, t.Status, t.Progress, formatLabelsShort(t.Labels), timeout, took, t.CreatedAt.Format(timeFmt), result, errMsg)
							}
							_ = w.Flush()
						})
					}
					resp, err := c.ListTasks(ctx, taskType, opts)
					if err != nil {
						return err
					}
					sort.Slice(resp.Tasks, func(i, j int) bool {
						return resp.Tasks[i].CreatedAt.After(resp.Tasks[j].CreatedAt)
					})
					return output(cmd, resp, func() {
						w := tab()
						_, _ = fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tPROGRESS\tTIMEOUT\tTOOK\tCREATED")
						for _, t := range resp.Tasks {
							took := "-"
							if t.CompletedAt != nil {
								took = fmtDuration(t.CompletedAt.Sub(t.CreatedAt))
							} else if t.StartedAt != nil {
								took = fmtDuration(time.Since(*t.StartedAt))
							}
							timeout := "-"
							if t.Timeout != "" {
								timeout = t.Timeout
							}
							_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d%%\t%s\t%s\t%s\n",
								t.ID, t.Type, t.Status, t.Progress, timeout, took, t.CreatedAt.Format(timeFmt))
						}
						_ = w.Flush()
					})
				},
			},
			{
				Name:      "get",
				Usage:     "get task details",
				ArgsUsage: "<id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					id := cmd.Args().First()
					if id == "" {
						return fmt.Errorf("task ID required")
					}
					resp, err := clientFrom(cmd).GetTask(ctx, id)
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
						fmt.Printf("Created:    %s\n", resp.CreatedAt.Format(timeFmt))
						if resp.StartedAt != nil {
							fmt.Printf("Started:    %s\n", resp.StartedAt.Format(timeFmt))
						}
						if resp.CompletedAt != nil {
							fmt.Printf("Completed:  %s\n", resp.CompletedAt.Format(timeFmt))
							fmt.Printf("Took:       %s\n", fmtDuration(resp.CompletedAt.Sub(resp.CreatedAt)))
						}
						if s := taskResultSummary(resp); s != "" {
							fmt.Printf("Result:     %s\n", s)
						}
					})
				},
			},
			{
				Name:      "cancel",
				Usage:     "cancel a running task",
				ArgsUsage: "<id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					id := cmd.Args().First()
					if id == "" {
						return fmt.Errorf("task ID required")
					}
					if err := clientFrom(cmd).CancelTask(ctx, id); err != nil {
						return wrapErr(err, "task", id)
					}
					return output(cmd, map[string]string{"cancel_requested": id}, func() { fmt.Printf("task %q cancel requested\n", id) })
				},
			},
			{
				Name:  "create",
				Usage: "create a background task",
				Commands: []*cli.Command{
					{
						Name:  v1.TaskTypeScrub,
						Usage: "start a btrfs scrub",
						Flags: []cli.Flag{
							&cli.DurationFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "timeout (e.g. 1h, 30m)"},
							&cli.BoolFlag{Name: "wait", Aliases: []string{"w"}, Usage: "wait for completion"},
							labelFlag(),
						},
						Action: func(ctx context.Context, cmd *cli.Command) error {
							c := clientFrom(cmd)
							req := v1.TaskCreateRequest{Labels: labelsWithDefault(cmd, "created-by", "cli")}
							if t := cmd.Duration("timeout"); t > 0 {
								req.Timeout = t.String()
							}
							resp, err := c.CreateTask(ctx, v1.TaskTypeScrub, req)
							if err != nil {
								return err
							}
							if !cmd.Bool("wait") {
								return output(cmd, resp, func() {
									fmt.Printf("scrub started (task %s)\n", resp.TaskID)
								})
							}
							fmt.Printf("scrub started (task %s)\n", resp.TaskID)
							return waitForTask(ctx, c, resp.TaskID)
						},
					},
					{
						Name:  v1.TaskTypeTest,
						Usage: "start a test task",
						Flags: []cli.Flag{
							&cli.DurationFlag{Name: "sleep", Aliases: []string{"s"}, Usage: "sleep duration (e.g. 10s, 1m)"},
							&cli.DurationFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "timeout (e.g. 1h, 30m)"},
							&cli.BoolFlag{Name: "wait", Aliases: []string{"w"}, Usage: "wait for completion"},
							labelFlag(),
						},
						Action: func(ctx context.Context, cmd *cli.Command) error {
							c := clientFrom(cmd)
							req := v1.TaskCreateRequest{Labels: labelsWithDefault(cmd, "created-by", "cli")}
							if s := cmd.Duration("sleep"); s > 0 {
								req.Opts = map[string]string{"sleep": s.String()}
							}
							if t := cmd.Duration("timeout"); t > 0 {
								req.Timeout = t.String()
							}
							resp, err := c.CreateTask(ctx, v1.TaskTypeTest, req)
							if err != nil {
								return err
							}
							if !cmd.Bool("wait") {
								return output(cmd, resp, func() {
									fmt.Printf("test task started (task %s)\n", resp.TaskID)
								})
							}
							fmt.Printf("test task started (task %s)\n", resp.TaskID)
							return waitForTask(ctx, c, resp.TaskID)
						},
					},
				},
			},
		},
	}
}

func waitForTask(ctx context.Context, c *v1.Client, id string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			t, err := c.GetTask(ctx, id)
			if err != nil {
				return wrapErr(err, "task", id)
			}
			switch t.Status {
			case v1.TaskStatusCompleted:
				took := "-"
				if t.CompletedAt != nil {
					took = fmtDuration(t.CompletedAt.Sub(t.CreatedAt))
				}
				if s := taskResultSummary(t); s != "" {
					fmt.Printf("completed (took %s, %s)\n", took, s)
				} else {
					fmt.Printf("completed (took %s)\n", took)
				}
				return nil
			case v1.TaskStatusFailed:
				return fmt.Errorf("task failed: %s", t.Error)
			case v1.TaskStatusCancelled:
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
