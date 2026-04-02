package ctl

import (
	"context"
	"fmt"
	"sort"
	"time"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/urfave/cli/v3"
)

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
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					taskType := cmd.String("type")
					if isWide(cmd) {
						resp, err := c.ListTasksDetail(ctx, taskType)
						if err != nil {
							return err
						}
						sort.Slice(resp.Tasks, func(i, j int) bool {
							return resp.Tasks[i].CreatedAt.After(resp.Tasks[j].CreatedAt)
						})
						return output(cmd, resp, func() {
							w := tab()
							fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tPROGRESS\tTOOK\tCREATED\tINFO")
							for _, t := range resp.Tasks {
								took := "-"
								if t.CompletedAt != nil {
									took = fmtDuration(t.CompletedAt.Sub(t.CreatedAt))
								} else if t.StartedAt != nil {
									took = fmtDuration(time.Since(*t.StartedAt))
								}
								info := t.Info
								if info == "" {
									info = "-"
								}
								fmt.Fprintf(w, "%s\t%s\t%s\t%d%%\t%s\t%s\t%s\n",
									t.ID, t.Type, t.Status, t.Progress, took, t.CreatedAt.Format(timeFmt), info)
							}
							w.Flush()
						})
					}
					resp, err := c.ListTasks(ctx, taskType)
					if err != nil {
						return err
					}
					sort.Slice(resp.Tasks, func(i, j int) bool {
						return resp.Tasks[i].CreatedAt.After(resp.Tasks[j].CreatedAt)
					})
					return output(cmd, resp, func() {
						w := tab()
						fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tPROGRESS\tTOOK\tCREATED")
						for _, t := range resp.Tasks {
							took := "-"
							if t.CompletedAt != nil {
								took = fmtDuration(t.CompletedAt.Sub(t.CreatedAt))
							} else if t.StartedAt != nil {
								took = fmtDuration(time.Since(*t.StartedAt))
							}
							fmt.Fprintf(w, "%s\t%s\t%s\t%d%%\t%s\t%s\n",
								t.ID, t.Type, t.Status, t.Progress, took, t.CreatedAt.Format(timeFmt))
						}
						w.Flush()
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
						if resp.Info != "" {
							fmt.Printf("Result:     %s\n", resp.Info)
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
							&cli.BoolFlag{Name: "wait", Aliases: []string{"w"}, Usage: "wait for completion"},
						},
						Action: func(ctx context.Context, cmd *cli.Command) error {
							c := clientFrom(cmd)
							resp, err := c.CreateTask(ctx, v1.TaskTypeScrub, nil)
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
				if t.Info != "" {
					fmt.Printf("completed (took %s, %s)\n", took, t.Info)
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
				if t.Info != "" {
					fmt.Printf("%s %d%% (%s)\n", t.Status, t.Progress, t.Info)
				} else {
					fmt.Printf("%s %d%%\n", t.Status, t.Progress)
				}
			}
		}
	}
}
