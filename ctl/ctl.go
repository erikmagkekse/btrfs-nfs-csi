package ctl

import (
	"context"
	"fmt"
	"os"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
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
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "agent-url", Sources: cli.EnvVars("AGENT_URL"), Usage: "agent API URL"},
			&cli.StringFlag{Name: "agent-token", Sources: cli.EnvVars("AGENT_TOKEN"), Usage: "tenant token"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: outputTable, Usage: "output format: table, wide, json, json,wide"},
		},
		Commands: []*cli.Command{
			volumeCmd(),
			snapshotCmd(),
			cloneCmd(),
			exportCmd(),
			taskCmd(),
			{
				Name:  "version",
				Usage: "show CLI version",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					fmt.Printf("btrfs-nfs-csi %s (%s)\n", Version, Commit)
					return nil
				},
			},
			{
				Name:  "stats",
				Usage: "show filesystem stats",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					resp, err := clientFrom(cmd).Stats(ctx)
					if err != nil {
						return err
					}
					return output(cmd, resp, func() {
						fmt.Println("statfs:")
						fmt.Printf("  Total:       %s\n", utils.FormatBytes(resp.Statfs.TotalBytes))
						fmt.Printf("  Used:        %s (%.0f%%)\n", utils.FormatBytes(resp.Statfs.UsedBytes), usedPct(resp.Statfs.UsedBytes, resp.Statfs.TotalBytes))
						fmt.Printf("  Free:        %s\n", utils.FormatBytes(resp.Statfs.FreeBytes))
						fmt.Println()
						fmt.Println("btrfs:")
						fmt.Printf("  Total:       %s\n", utils.FormatBytes(resp.Btrfs.TotalBytes))
						fmt.Printf("  Used:        %s (%.0f%%)\n", utils.FormatBytes(resp.Btrfs.UsedBytes), usedPct(resp.Btrfs.UsedBytes, resp.Btrfs.TotalBytes))
						fmt.Printf("  Free:        %s\n", utils.FormatBytes(resp.Btrfs.FreeBytes))
						fmt.Printf("  Unallocated: %s\n", utils.FormatBytes(resp.Btrfs.UnallocatedBytes))
						fmt.Printf("  Metadata:    %s / %s\n", utils.FormatBytes(resp.Btrfs.MetadataUsedBytes), utils.FormatBytes(resp.Btrfs.MetadataTotalBytes))
						fmt.Printf("  Data ratio:  %.1f\n", resp.Btrfs.DataRatio)
						fmt.Println()
						fmt.Println("devices:")
						w := tab()
						if isWide(cmd) {
							_, _ = fmt.Fprintln(w, "  DEVICE\tSIZE\tALLOCATED\tREAD\tWRITTEN\tREAD_IOS\tWRITE_IOS\tREAD_ERR\tWRITE_ERR\tFLUSH_ERR\tCSUM_ERR\tGEN_ERR\tSTATUS")
							for _, d := range resp.Btrfs.Devices {
								status := "ok"
								if d.Missing {
									status = "MISSING"
								} else if d.Errors.ReadErrs+d.Errors.WriteErrs+d.Errors.FlushErrs+d.Errors.CorruptionErrs+d.Errors.GenerationErrs > 0 {
									status = "ERRORS"
								}
								_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
									d.Device, utils.FormatBytes(d.SizeBytes), utils.FormatBytes(d.AllocatedBytes),
									utils.FormatBytes(d.IO.ReadBytesTotal), utils.FormatBytes(d.IO.WriteBytesTotal),
									d.IO.ReadIOsTotal, d.IO.WriteIOsTotal,
									d.Errors.ReadErrs, d.Errors.WriteErrs, d.Errors.FlushErrs,
									d.Errors.CorruptionErrs, d.Errors.GenerationErrs, status)
							}
						} else {
							_, _ = fmt.Fprintln(w, "  DEVICE\tSIZE\tALLOCATED\tREAD\tWRITTEN\tERRORS\tSTATUS")
							for _, d := range resp.Btrfs.Devices {
								errs := d.Errors.ReadErrs + d.Errors.WriteErrs + d.Errors.FlushErrs + d.Errors.CorruptionErrs + d.Errors.GenerationErrs
								status := "ok"
								if d.Missing {
									status = "MISSING"
								} else if errs > 0 {
									status = "ERRORS"
								}
								_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%d\t%s\n",
									d.Device, utils.FormatBytes(d.SizeBytes), utils.FormatBytes(d.AllocatedBytes),
									utils.FormatBytes(d.IO.ReadBytesTotal), utils.FormatBytes(d.IO.WriteBytesTotal),
									errs, status)
							}
						}
						_ = w.Flush()
					})
				},
			},
			{
				Name:  "health",
				Usage: "show agent health",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					resp, err := clientFrom(cmd).Healthz(ctx)
					if err != nil {
						return err
					}
					return output(cmd, resp, func() {
						fmt.Printf("status:  %s\nversion: %s\ncommit:  %s\nuptime:  %ds\n",
							resp.Status, resp.Version, resp.Commit, resp.UptimeSeconds)
					})
				},
			},
		},
	}

	if err := app.Run(context.Background(), append([]string{"btrfs-nfs-csi"}, args...)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
