package ctl

import (
	"context"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/urfave/cli/v3"
)

func volumeCmd() *cli.Command {
	return &cli.Command{
		Name:    "volume",
		Aliases: []string{"volumes", "vol"},
		Usage:   "manage volumes",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list all volumes",
				Flags:   []cli.Flag{sortFlag(), ascFlag(), labelFlag(), columnsFlag(), watchFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortUsedPct
					}
					rev := !cmd.Bool("asc")
					opts := v1.ListOpts{Labels: splitLabelsFlag(cmd)}
					return runWatch(ctx, cmd, func() error {
						return listVolumes(ctx, cmd, c, sortBy, rev, opts)
					})
				},
			},
			{
				Name:      "get",
				Usage:     "get volume details",
				ArgsUsage: "<name>",
				Action:    volumeGet,
			},
			{
				Name:      "create",
				Usage:     "create a volume",
				ArgsUsage: "<name> <size>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "compression", Aliases: []string{"c"}, Usage: "compression: zstd, lzo, zlib"},
					&cli.BoolFlag{Name: "nocow", Aliases: []string{"C"}, Usage: "disable copy-on-write (for databases)"},
					&cli.IntFlag{Name: "uid", Aliases: []string{"u"}, Usage: "owner UID"},
					&cli.IntFlag{Name: "gid", Aliases: []string{"g"}, Usage: "owner GID"},
					&cli.StringFlag{Name: "mode", Aliases: []string{"m"}, Usage: "directory mode (octal)"},
					labelFlag(),
				},
				Action: volumeCreate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"rm"},
				Usage:     "delete volumes",
				ArgsUsage: "<name> [name...]",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "confirm", Hidden: true},
					&cli.BoolFlag{Name: "yes", Hidden: true},
				},
				Action: volumeDelete,
			},
			{
				Name:      "expand",
				Usage:     "resize a volume",
				ArgsUsage: "<name> <size|+size|-size>",
				Action:    volumeExpand,
			},
			{
				Name:      "clone",
				Usage:     "clone a volume (PVC-to-PVC)",
				ArgsUsage: "<source> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    volumeClone,
			},
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
				Flags:     []cli.Flag{sortFlag(), ascFlag(), labelFlag(), columnsFlag(), watchFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortCreated
					}
					rev := !cmd.Bool("asc")
					vol := cmd.Args().First()
					opts := v1.ListOpts{Labels: splitLabelsFlag(cmd)}
					return runWatch(ctx, cmd, func() error {
						return listSnapshots(ctx, cmd, c, vol, sortBy, rev, opts)
					})
				},
			},
			{
				Name:      "get",
				Usage:     "get snapshot details",
				ArgsUsage: "<name>",
				Action:    snapshotGet,
			},
			{
				Name:      "create",
				Usage:     "create a snapshot",
				ArgsUsage: "<volume> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    snapshotCreate,
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
				Action: snapshotDelete,
			},
			{
				Name:      "clone",
				Usage:     "create writable clone from snapshot",
				ArgsUsage: "<snapshot> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    snapshotClone,
			},
		},
	}
}

func exportCmd() *cli.Command {
	return &cli.Command{
		Name:    "export",
		Aliases: []string{"exports"},
		Usage:   "manage NFS exports",
		Commands: []*cli.Command{
			{
				Name:      "add",
				Usage:     "add NFS export",
				ArgsUsage: "<volume> <client-ip>",
				Action:    exportAdd,
			},
			{
				Name:      "remove",
				Aliases:   []string{"rm"},
				Usage:     "remove NFS export",
				ArgsUsage: "<volume> <client-ip>",
				Action:    exportRemove,
			},
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list active NFS exports",
				Flags:   []cli.Flag{columnsFlag(), watchFlag()},
				Action:  exportList,
			},
		},
	}
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
					columnsFlag(),
					watchFlag(),
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					taskType := cmd.String("type")
					opts := v1.ListOpts{Labels: splitLabelsFlag(cmd)}
					return runWatch(ctx, cmd, func() error {
						return listTasks(ctx, cmd, c, taskType, opts)
					})
				},
			},
			{
				Name:      "get",
				Usage:     "get task details",
				ArgsUsage: "<id>",
				Action:    taskGet,
			},
			{
				Name:      "cancel",
				Usage:     "cancel a running task",
				ArgsUsage: "<id>",
				Action:    taskCancel,
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
							&cli.BoolFlag{Name: "wait", Aliases: []string{"W"}, Usage: "wait for completion"},
							labelFlag(),
						},
						Action: taskCreateScrub,
					},
					{
						Name:  v1.TaskTypeTest,
						Usage: "start a test task",
						Flags: []cli.Flag{
							&cli.DurationFlag{Name: "sleep", Aliases: []string{"s"}, Usage: "sleep duration (e.g. 10s, 1m)"},
							&cli.DurationFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "timeout (e.g. 1h, 30m)"},
							&cli.BoolFlag{Name: "wait", Aliases: []string{"W"}, Usage: "wait for completion"},
							labelFlag(),
						},
						Action: taskCreateTest,
					},
				},
			},
		},
	}
}
