package main

import (
	"context"
	"fmt"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/urfave/cli/v3"
)

func versionCmd() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "show CLI version",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Printf("Local:\n")
			fmt.Printf("  btrfs-nfs-csi %s (%s)\n", version, commit)
			if cmd.String("agent-url") != "" && cmd.String("agent-token") != "" {
				fmt.Printf("Agent:\n")
				if h, err := apiClient.Healthz(ctx); err == nil {
					fmt.Printf("  btrfs-nfs-csi %s (%s)\n", h.Version, h.Commit)
				} else {
					fmt.Printf("  unreachable (%v)\n", err)
				}
			}
			return nil
		},
	}
}

func statsCmd() *cli.Command {
	return &cli.Command{
		Name:   "stats",
		Usage:  "show filesystem stats",
		Flags:  []cli.Flag{watchFlag()},
		Action: watchAction(showStats),
	}
}

func healthCmd() *cli.Command {
	return &cli.Command{
		Name:   "health",
		Usage:  "show agent health",
		Flags:  []cli.Flag{watchFlag()},
		Action: watchAction(showHealth),
	}
}

func volumeCmd() *cli.Command {
	return &cli.Command{
		Name:    "volume",
		Aliases: []string{"volumes", "vol", "v"},
		Usage:   "manage volumes",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls", "l"},
				Usage:   "list all volumes",
				Flags:   []cli.Flag{allFlag(), sortFlag(), ascFlag(), labelFlag(), columnsFlag(), watchFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortUsedPct
					}
					rev := !cmd.Bool("asc")
					opts := buildListOpts(cmd)
					return runWatch(ctx, cmd, func() error {
						return listVolumes(ctx, cmd, sortBy, rev, opts)
					})
				},
			},
			{
				Name:      "get",
				Aliases:   []string{"g"},
				Usage:     "get volume details",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{watchFlag()},
				Action:    watchAction(volumeGet),
			},
			{
				Name:      "create",
				Aliases:   []string{"c"},
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
				Aliases:   []string{"rm", "d"},
				Usage:     "delete volumes",
				ArgsUsage: "<name> [name...]",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "confirm", Hidden: true},
					&cli.BoolFlag{Name: "yes", Hidden: true},
				},
				Action: volumeDelete,
			},
			{
				Name:      "set",
				Aliases:   []string{"s"},
				Usage:     "update volume properties",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "uid", Aliases: []string{"u"}, Usage: "owner UID"},
					&cli.IntFlag{Name: "gid", Aliases: []string{"g"}, Usage: "owner GID"},
					&cli.StringFlag{Name: "mode", Aliases: []string{"m"}, Usage: "directory mode (octal)"},
					&cli.StringFlag{Name: "compression", Usage: "compression: zstd, lzo, zlib, none"},
					&cli.BoolFlag{Name: "nocow", Usage: "disable copy-on-write"},
				},
				Action: volumeSet,
			},
			{
				Name:      "expand",
				Aliases:   []string{"e"},
				Usage:     "resize a volume",
				ArgsUsage: "<name> <size|+size|-size>",
				Action:    volumeExpand,
			},
			{
				Name:      "clone",
				Aliases:   []string{"cl"},
				Usage:     "clone a volume (PVC-to-PVC)",
				ArgsUsage: "<source> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    volumeClone,
			},
			{
				Name:    "label",
				Aliases: []string{"labels", "lb"},
				Usage:   "manage volume labels",
				Commands: []*cli.Command{
					{
						Name:      "list",
						Aliases:   []string{"ls", "l"},
						Usage:     "list labels on a volume",
						ArgsUsage: "<name>",
						Action:    volumeLabelList,
					},
					{
						Name:      "add",
						Aliases:   []string{"a"},
						Usage:     "add or update labels on a volume",
						ArgsUsage: "<name> key=value [key=value...]",
						Action:    volumeLabelAdd,
					},
					{
						Name:      "remove",
						Aliases:   []string{"rm", "r"},
						Usage:     "remove labels from a volume",
						ArgsUsage: "<name> key[=value] [key[=value]...]",
						Action:    volumeLabelRemove,
					},
					{
						Name:      "patch",
						Aliases:   []string{"p"},
						Usage:     "replace all labels (preserves reserved labels)",
						ArgsUsage: "<name> [key=value...]",
						Action:    volumeLabelPatch,
					},
				},
			},
		},
	}
}

func snapshotCmd() *cli.Command {
	return &cli.Command{
		Name:    "snapshot",
		Aliases: []string{"snapshots", "snap", "s"},
		Usage:   "manage snapshots",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Aliases:   []string{"ls", "l"},
				Usage:     "list snapshots (optionally filter by volume)",
				ArgsUsage: "[volume]",
				Flags:     []cli.Flag{allFlag(), sortFlag(), ascFlag(), labelFlag(), columnsFlag(), watchFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortCreated
					}
					rev := !cmd.Bool("asc")
					vol := cmd.Args().First()
					opts := buildListOpts(cmd)
					return runWatch(ctx, cmd, func() error {
						return listSnapshots(ctx, cmd, vol, sortBy, rev, opts)
					})
				},
			},
			{
				Name:      "get",
				Aliases:   []string{"g"},
				Usage:     "get snapshot details",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{watchFlag()},
				Action:    watchAction(snapshotGet),
			},
			{
				Name:      "create",
				Aliases:   []string{"c"},
				Usage:     "create a snapshot",
				ArgsUsage: "<volume> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    snapshotCreate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"rm", "d"},
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
		Aliases: []string{"exports", "e"},
		Usage:   "manage NFS exports",
		Commands: []*cli.Command{
			{
				Name:      "add",
				Aliases:   []string{"a"},
				Usage:     "add NFS export",
				ArgsUsage: "<volume> <client-ip>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    exportAdd,
			},
			{
				Name:      "remove",
				Aliases:   []string{"rm", "r"},
				Usage:     "remove NFS export",
				ArgsUsage: "<volume> <client-ip>",
				Flags:     []cli.Flag{labelFlag()},
				Action:    exportRemove,
			},
			{
				Name:    "list",
				Aliases: []string{"ls", "l"},
				Usage:   "list active NFS exports",
				Flags:   []cli.Flag{allFlag(), sortFlag(), ascFlag(), labelFlag(), columnsFlag(), watchFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sortBy := cmd.String("sort")
					rev := !cmd.Bool("asc")
					opts := buildListOpts(cmd)
					return runWatch(ctx, cmd, func() error {
						return listExports(ctx, cmd, sortBy, rev, opts)
					})
				},
			},
		},
	}
}

func taskCmd() *cli.Command {
	return &cli.Command{
		Name:    "task",
		Aliases: []string{"tasks", "t"},
		Usage:   "manage background tasks",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls", "l"},
				Usage:   "list tasks",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "filter by type (e.g. scrub)"},
					allFlag(),
					sortFlag(),
					ascFlag(),
					labelFlag(),
					columnsFlag(),
					watchFlag(),
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					taskType := cmd.String("type")
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortCreated
					}
					rev := !cmd.Bool("asc")
					opts := buildListOpts(cmd)
					return runWatch(ctx, cmd, func() error {
						return listTasks(ctx, cmd, taskType, sortBy, rev, opts)
					})
				},
			},
			{
				Name:      "get",
				Aliases:   []string{"g"},
				Usage:     "get task details",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{watchFlag()},
				Action:    watchAction(taskGet),
			},
			{
				Name:      "cancel",
				Usage:     "cancel a running task",
				ArgsUsage: "<id>",
				Action:    taskCancel,
			},
			{
				Name:    "create",
				Aliases: []string{"c"},
				Usage:   "create a background task",
				Commands: []*cli.Command{
					{
						Name:  models.TaskTypeScrub,
						Usage: "start a btrfs scrub",
						Flags: []cli.Flag{
							&cli.DurationFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "timeout (e.g. 1h, 30m)"},
							&cli.BoolFlag{Name: "wait", Aliases: []string{"W"}, Usage: "wait for completion"},
							labelFlag(),
						},
						Action: taskCreateScrub,
					},
					{
						Name:  models.TaskTypeTest,
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
