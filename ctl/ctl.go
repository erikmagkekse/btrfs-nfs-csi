package ctl

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

func Run(args []string) {
	app := &cli.Command{
		Name:  "btrfs-nfs-csi",
		Usage: "btrfs-nfs-csi agent CLI",
		OnUsageError: func(_ context.Context, _ *cli.Command, err error, _ bool) error {
			return err
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			initClient(cmd)
			cmdStart = time.Now()
			return ctx, nil
		},
		After: func(ctx context.Context, cmd *cli.Command) error {
			if !cmdStart.IsZero() && !ranWatch && !isJSON(cmd) && cmd.String("columns") == "" {
				timingLine = fmtTiming(time.Since(cmdStart))
			}
			return nil
		},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "agent-url", Sources: cli.EnvVars("AGENT_URL"), Usage: "agent API URL"},
			&cli.StringFlag{Name: "agent-token", Sources: cli.EnvVars("AGENT_TOKEN"), Usage: "tenant token"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: outputTable, Usage: "output format: table, wide, json, json,wide"},
		},
		Commands: []*cli.Command{
			volumeCmd(),
			snapshotCmd(),
			exportCmd(),
			taskCmd(),
			versionCmd(),
			statsCmd(),
			healthCmd(),
		},
	}

	if err := app.Run(context.Background(), append([]string{"btrfs-nfs-csi"}, injectWatchDefault(args)...)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if timingLine != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", timingLine)
	}
}
