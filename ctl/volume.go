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

func volumeCmd() *cli.Command {
	return &cli.Command{
		Name:    "volume",
		Aliases: []string{"volumes", "vol"},
		Usage:   "manage volumes",
		Commands: []*cli.Command{

			// --- list ---

			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list all volumes",
				Flags:   []cli.Flag{sortFlag(), ascFlag(), labelFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					c := clientFrom(cmd)
					sortBy := cmd.String("sort")
					if sortBy == "" {
						sortBy = sortUsedPct
					}
					rev := !cmd.Bool("asc")
					opts := v1.ListOpts{Labels: cmd.StringSlice("label")}

					if isWide(cmd) {
						resp, err := c.ListVolumesDetail(ctx, opts)
						if err != nil {
							return err
						}
						sortVolumesDetail(resp.Volumes, sortBy, rev)
						return output(cmd, resp, func() {
							w := tab()
							_, _ = fmt.Fprintln(w, "NAME\tSIZE\tUSED\tQUOTA\tCOMPRESSION\tNOCOW\tUID\tGID\tMODE\tLABELS\tCLIENTS\tCREATED")
							for _, v := range resp.Volumes {
								_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\t%d\t%d\t%s\t%s\t%d\t%s\n",
									v.Name, utils.FormatBytes(v.SizeBytes), utils.FormatBytes(v.UsedBytes), utils.FormatBytes(v.QuotaBytes),
									v.Compression, v.NoCOW, v.UID, v.GID, v.Mode,
									formatLabelsShort(v.Labels), len(v.Clients), v.CreatedAt.Format(timeFmt))
							}
							_ = w.Flush()
						})
					}

					resp, err := c.ListVolumes(ctx, opts)
					if err != nil {
						return err
					}
					sortVolumes(resp.Volumes, sortBy, rev)
					return output(cmd, resp, func() {
						w := tab()
						_, _ = fmt.Fprintln(w, "NAME\tSIZE\tUSED\tUSED%\tCLIENTS\tCREATED")
						for _, v := range resp.Volumes {
							_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%.0f%%\t%d\t%s\n",
								v.Name, utils.FormatBytes(v.SizeBytes), utils.FormatBytes(v.UsedBytes),
								usedPct(v.UsedBytes, v.SizeBytes), v.Clients, v.CreatedAt.Format(timeFmt))
						}
						_ = w.Flush()
					})
				},
			},

			// --- get ---

			{
				Name:      "get",
				Usage:     "get volume details",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					name := cmd.Args().First()
					if name == "" {
						return fmt.Errorf("volume name required")
					}

					resp, err := clientFrom(cmd).GetVolume(ctx, name)
					if err != nil {
						return wrapErr(err, "volume", name)
					}

					return output(cmd, resp, func() {
						fmt.Printf("Name:         %s\n", resp.Name)
						fmt.Printf("Size:         %s\n", utils.FormatBytes(resp.SizeBytes))
						fmt.Printf("Used:         %s (%.0f%%)\n", utils.FormatBytes(resp.UsedBytes), usedPct(resp.UsedBytes, resp.SizeBytes))
						fmt.Printf("Quota:        %s\n", utils.FormatBytes(resp.QuotaBytes))
						fmt.Printf("Compression:  %s\n", resp.Compression)
						fmt.Printf("NoCOW:        %v\n", resp.NoCOW)
						fmt.Printf("UID:          %d\n", resp.UID)
						fmt.Printf("GID:          %d\n", resp.GID)
						fmt.Printf("Mode:         %s\n", resp.Mode)
						printLabels("Labels:", resp.Labels, 14)
						if len(resp.Clients) == 0 {
							fmt.Printf("Clients:      none\n")
						} else {
							fmt.Printf("Clients:      %s\n", strings.Join(resp.Clients, ", "))
						}
						fmt.Printf("Created:      %s\n", resp.CreatedAt.Format(timeFmt))
						fmt.Printf("Updated:      %s\n", resp.UpdatedAt.Format(timeFmt))
					})
				},
			},

			// --- create ---

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
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: volume create <name> <size>")
					}

					size, err := utils.ParseSize(cmd.Args().Get(1))
					if err != nil {
						return err
					}

					compression := cmd.String("compression")
					if compression != "" && !utils.IsValidCompression(compression) {
						return fmt.Errorf("invalid compression %q, expected: zstd, lzo, zlib (with optional level, e.g. zstd:3)", compression)
					}

					req := v1.VolumeCreateRequest{
						Name:        cmd.Args().Get(0),
						SizeBytes:   size,
						Compression: compression,
						NoCOW:       cmd.Bool("nocow"),
						UID:         int(cmd.Int("uid")),
						GID:         int(cmd.Int("gid")),
						Mode:        cmd.String("mode"),
						Labels:      labelsWithDefault(cmd, "created-by", "cli"),
					}

					resp, err := clientFrom(cmd).CreateVolume(ctx, req)
					if err != nil {
						return wrapErr(err, "volume", req.Name)
					}
					return output(cmd, resp, func() {
						fmt.Printf("volume %q created (%s)\n", resp.Name, utils.FormatBytes(resp.SizeBytes))
					})
				},
			},

			// --- delete ---

			{
				Name:      "delete",
				Aliases:   []string{"rm"},
				Usage:     "delete volumes",
				ArgsUsage: "<name> [name...]",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "confirm", Hidden: true},
					&cli.BoolFlag{Name: "yes", Hidden: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					names := cmd.Args().Slice()
					if len(names) == 0 {
						return fmt.Errorf("volume name required")
					}

					force := os.Getenv("BTRFS_NFS_CSI_FORCE") == "true"
					if !force && !cmd.Bool("confirm") {
						_, _ = fmt.Fprintf(os.Stderr, "to delete, run:\n  btrfs-nfs-csi volume delete %s --confirm\n", strings.Join(names, " "))
						return nil
					}
					if !force && !cmd.Bool("yes") {
						_, _ = fmt.Fprintf(os.Stderr, "this will permanently destroy all data.\nto proceed, run:\n  btrfs-nfs-csi volume delete %s --confirm --yes\n", strings.Join(names, " "))
						return nil
					}

					c := clientFrom(cmd)
					deleted := make([]string, 0, len(names))
					for _, name := range names {
						if err := c.DeleteVolume(ctx, name); err != nil {
							return wrapErr(err, "volume", name)
						}
						deleted = append(deleted, name)
					}
					return output(cmd, map[string]any{"deleted": deleted}, func() {
						for _, name := range deleted {
							fmt.Printf("volume %q deleted\n", name)
						}
					})
				},
			},

			// --- expand ---

			{
				Name:      "expand",
				Usage:     "expand a volume",
				ArgsUsage: "<name> <new-size>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: volume expand <name> <new-size>")
					}

					size, err := utils.ParseSize(cmd.Args().Get(1))
					if err != nil {
						return err
					}

					resp, err := clientFrom(cmd).UpdateVolume(ctx, cmd.Args().Get(0), v1.VolumeUpdateRequest{SizeBytes: &size})
					if err != nil {
						return wrapErr(err, "volume", cmd.Args().Get(0))
					}
					return output(cmd, resp, func() {
						fmt.Printf("volume %q expanded to %s\n", resp.Name, utils.FormatBytes(resp.SizeBytes))
					})
				},
			},

			// --- clone ---

			{
				Name:      "clone",
				Usage:     "clone a volume (PVC-to-PVC)",
				ArgsUsage: "<source> <name>",
				Flags:     []cli.Flag{labelFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: volume clone <source> <name>")
					}

					resp, err := clientFrom(cmd).CloneVolume(ctx, v1.VolumeCloneRequest{Source: cmd.Args().Get(0), Name: cmd.Args().Get(1), Labels: parseLabelsFlag(cmd)})
					if err != nil {
						return wrapErr(err, "volume", cmd.Args().Get(1))
					}
					return output(cmd, resp, func() {
						fmt.Printf("volume %q cloned from %q\n", resp.Name, cmd.Args().Get(0))
					})
				},
			},
		},
	}
}
