package ctl

import (
	"context"
	"fmt"
	"os"
	"strings"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/urfave/cli/v3"
)

func snapshotCloneCmd() *cli.Command {
	return &cli.Command{
		Name:      "clone",
		Usage:     "create writable clone from snapshot",
		ArgsUsage: "<snapshot> <name>",
		Flags:     []cli.Flag{labelFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 2 {
				return fmt.Errorf("usage: snapshot clone <snapshot> <name>")
			}
			resp, err := clientFrom(cmd).CreateClone(ctx, v1.CloneCreateRequest{Snapshot: cmd.Args().Get(0), Name: cmd.Args().Get(1), Labels: parseLabelsFlag(cmd)})
			if err != nil {
				return wrapErr(err, "clone", cmd.Args().Get(1))
			}
			return output(cmd, resp, func() { fmt.Printf("clone %q created from snapshot %q\n", resp.Name, resp.SourceSnapshot) })
		},
	}
}

func snapshotCmd() *cli.Command {
	return &cli.Command{
		Name:    "snapshot",
		Aliases: []string{"snapshots", "snap"},
		Usage:   "manage snapshots",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Aliases:   []string{"ls"},
				Usage:     "list snapshots (optionally filter by volume)",
				ArgsUsage: "[volume]",
				Flags:     []cli.Flag{sortFlag(), ascFlag(), labelFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortCreated
					}
					rev := !cmd.Bool("asc")
					vol := cmd.Args().First()
					labels := cmd.StringSlice("label")
					if isWide(cmd) {
						var resp *v1.SnapshotDetailListResponse
						var err error
						if vol != "" {
							resp, err = c.ListVolumeSnapshotsDetail(ctx, vol, labels...)
						} else {
							resp, err = c.ListSnapshotsDetail(ctx, labels...)
						}
						if err != nil {
							return err
						}
						sortSnapshotsDetail(resp.Snapshots, sortBy, rev)
						return output(cmd, resp, func() {
							w := tab()
							_, _ = fmt.Fprintln(w, "NAME\tVOLUME\tSIZE\tUSED\tEXCLUSIVE\tREADONLY\tLABELS\tCREATED")
							for _, s := range resp.Snapshots {
								_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\t%s\t%s\n",
									s.Name, s.Volume, utils.FormatBytes(s.SizeBytes), utils.FormatBytes(s.UsedBytes),
									utils.FormatBytes(s.ExclusiveBytes), s.ReadOnly, formatLabelsShort(s.Labels), s.CreatedAt.Format(timeFmt))
							}
							_ = w.Flush()
						})
					}
					var resp *v1.SnapshotListResponse
					var err error
					if vol != "" {
						resp, err = c.ListVolumeSnapshots(ctx, vol, labels...)
					} else {
						resp, err = c.ListSnapshots(ctx, labels...)
					}
					if err != nil {
						return err
					}
					sortSnapshots(resp.Snapshots, sortBy, rev)
					return output(cmd, resp, func() {
						w := tab()
						_, _ = fmt.Fprintln(w, "NAME\tVOLUME\tSIZE\tUSED\tCREATED")
						for _, s := range resp.Snapshots {
							_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
								s.Name, s.Volume, utils.FormatBytes(s.SizeBytes), utils.FormatBytes(s.UsedBytes), s.CreatedAt.Format(timeFmt))
						}
						_ = w.Flush()
					})
				},
			},
			{
				Name:      "get",
				Usage:     "get snapshot details",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					name := cmd.Args().First()
					if name == "" {
						return fmt.Errorf("snapshot name required")
					}
					resp, err := clientFrom(cmd).GetSnapshot(ctx, name)
					if err != nil {
						return wrapErr(err, "snapshot", name)
					}
					return output(cmd, resp, func() {
						fmt.Printf("Name:       %s\n", resp.Name)
						fmt.Printf("Volume:     %s\n", resp.Volume)
						fmt.Printf("Size:       %s\n", utils.FormatBytes(resp.SizeBytes))
						fmt.Printf("Used:       %s\n", utils.FormatBytes(resp.UsedBytes))
						fmt.Printf("Exclusive:  %s\n", utils.FormatBytes(resp.ExclusiveBytes))
						fmt.Printf("ReadOnly:   %v\n", resp.ReadOnly)
						printLabels("Labels:", resp.Labels, 12)
						fmt.Printf("Created:    %s\n", resp.CreatedAt.Format(timeFmt))
						fmt.Printf("Updated:    %s\n", resp.UpdatedAt.Format(timeFmt))
					})
				},
			},
			{
				Name:      "create",
				Usage:     "create a snapshot",
				ArgsUsage: "<volume> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: snapshot create <volume> <name>")
					}
					resp, err := clientFrom(cmd).CreateSnapshot(ctx, v1.SnapshotCreateRequest{Volume: cmd.Args().Get(0), Name: cmd.Args().Get(1), Labels: labelsWithDefault(cmd, "created-by", "cli")})
					if err != nil {
						return wrapErr(err, "snapshot", cmd.Args().Get(1))
					}
					return output(cmd, resp, func() { fmt.Printf("snapshot %q created from volume %q\n", resp.Name, resp.Volume) })
				},
			},
			{
				Name:      "delete",
				Aliases:   []string{"rm"},
				Usage:     "delete snapshots",
				ArgsUsage: "<name> [name...]",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "confirm", Hidden: true},
					&cli.BoolFlag{Name: "yes", Hidden: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					names := cmd.Args().Slice()
					if len(names) == 0 {
						return fmt.Errorf("snapshot name required")
					}
					force := os.Getenv("BTRFS_NFS_CSI_FORCE") == "true"
					if !force && !cmd.Bool("confirm") {
						_, _ = fmt.Fprintf(os.Stderr, "to delete, run:\n  btrfs-nfs-csi snapshot delete %s --confirm\n", strings.Join(names, " "))
						return nil
					}
					if !force && !cmd.Bool("yes") {
						_, _ = fmt.Fprintf(os.Stderr, "this will permanently destroy all snapshots.\nto proceed, run:\n  btrfs-nfs-csi snapshot delete %s --confirm --yes\n", strings.Join(names, " "))
						return nil
					}
					c := clientFrom(cmd)
					deleted := make([]string, 0, len(names))
					for _, name := range names {
						if err := c.DeleteSnapshot(ctx, name); err != nil {
							return wrapErr(err, "snapshot", name)
						}
						deleted = append(deleted, name)
					}
					return output(cmd, map[string]any{"deleted": deleted}, func() {
						for _, name := range deleted {
							fmt.Printf("snapshot %q deleted\n", name)
						}
					})
				},
			},
			snapshotCloneCmd(),
		},
	}
}
