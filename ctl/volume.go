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

func listVolumes(ctx context.Context, cmd *cli.Command, c *v1.Client, sortBy string, rev bool, opts v1.ListOpts) error {
	if isWide(cmd) {
		resp, err := c.ListVolumesDetail(ctx, opts)
		if err != nil {
			return err
		}
		sortVolumesDetail(resp.Volumes, sortBy, rev)
		return output(cmd, resp, func() {
			tw := newTableWriter(cmd, []string{"NAME", "SIZE", "USED", "QUOTA", "COMPRESSION", "NOCOW", "UID", "GID", "MODE", "LABELS", "CLIENTS", "CREATED"})
			tw.writeHeader()
			for _, v := range resp.Volumes {
				tw.writeRow(map[string]string{
					"NAME": v.Name, "SIZE": utils.FormatBytes(v.SizeBytes), "USED": utils.FormatBytes(v.UsedBytes),
					"QUOTA": utils.FormatBytes(v.QuotaBytes), "COMPRESSION": v.Compression, "NOCOW": fmt.Sprintf("%v", v.NoCOW),
					"UID": fmt.Sprintf("%d", v.UID), "GID": fmt.Sprintf("%d", v.GID), "MODE": v.Mode,
					"LABELS": formatLabelsShort(v.Labels), "CLIENTS": fmt.Sprintf("%d", len(v.Exports)), "CREATED": v.CreatedAt.Format(timeFmt),
				})
			}
			tw.flush()
		})
	}
	resp, err := c.ListVolumes(ctx, opts)
	if err != nil {
		return err
	}
	sortVolumes(resp.Volumes, sortBy, rev)
	return output(cmd, resp, func() {
		tw := newTableWriter(cmd, []string{"NAME", "SIZE", "USED", "USED%", "CLIENTS", "CREATED"})
		tw.writeHeader()
		for _, v := range resp.Volumes {
			tw.writeRow(map[string]string{
				"NAME": v.Name, "SIZE": utils.FormatBytes(v.SizeBytes), "USED": utils.FormatBytes(v.UsedBytes),
				"USED%":   fmt.Sprintf("%.0f%%", usedPct(v.UsedBytes, v.SizeBytes)),
				"CLIENTS": fmt.Sprintf("%d", v.Exports), "CREATED": v.CreatedAt.Format(timeFmt),
			})
		}
		tw.flush()
	})
}

func volumeGet(ctx context.Context, cmd *cli.Command) error {
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
		if len(resp.Exports) == 0 {
			fmt.Printf("Exports:      none\n")
		} else {
			fmt.Printf("Exports:      %s\n", formatExports(resp.Exports))
		}
		fmt.Printf("Created:      %s\n", resp.CreatedAt.Format(timeFmt))
		fmt.Printf("Updated:      %s\n", resp.UpdatedAt.Format(timeFmt))
	})
}

func volumeCreate(ctx context.Context, cmd *cli.Command) error {
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
		Labels:      labelsWithDefault(cmd, "created-by", cliIdentity()),
	}
	resp, err := clientFrom(cmd).CreateVolume(ctx, req)
	if err != nil {
		return wrapErr(err, "volume", req.Name)
	}
	return output(cmd, resp, func() {
		fmt.Printf("volume %q created (%s)\n", resp.Name, utils.FormatBytes(resp.SizeBytes))
	})
}

func volumeDelete(ctx context.Context, cmd *cli.Command) error {
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return fmt.Errorf("volume name required")
	}
	force := os.Getenv("BTRFS_NFS_CSI_FORCE") == "true"
	confirmed := force || (cmd.Bool("confirm") && cmd.Bool("yes"))
	c := clientFrom(cmd)
	var protected []string
	for _, name := range names {
		if !confirmed {
			vol, err := c.GetVolume(ctx, name)
			if err != nil {
				return wrapErr(err, "volume", name)
			}
			if vol.Labels["created-by"] != cliIdentity() {
				protected = append(protected, name)
				continue
			}
		}
		if err := c.DeleteVolume(ctx, name); err != nil {
			return wrapErr(err, "volume", name)
		}
		if !isJSON(cmd) {
			fmt.Printf("volume %q deleted\n", name)
		}
	}
	if len(protected) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "skipped %d protected volume(s) (created-by != cli):\n  btrfs-nfs-csi volume delete %s --confirm --yes\n", len(protected), strings.Join(protected, " "))
	}
	return nil
}

func volumeExpand(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: volume expand <name> <size|+size>")
	}
	c := clientFrom(cmd)
	name := cmd.Args().Get(0)
	sizeArg := cmd.Args().Get(1)
	var size uint64
	if (sizeArg[0] == '+' || sizeArg[0] == '-') && len(sizeArg) > 1 && sizeArg[1] >= '0' && sizeArg[1] <= '9' {
		delta, err := utils.ParseSize(sizeArg[1:])
		if err != nil {
			return err
		}
		vol, err := c.GetVolume(ctx, name)
		if err != nil {
			return wrapErr(err, "volume", name)
		}
		if sizeArg[0] == '+' {
			size = vol.SizeBytes + delta
		} else {
			if delta > vol.SizeBytes {
				return fmt.Errorf("cannot shrink below 0 (current %s, delta %s)", utils.FormatBytes(vol.SizeBytes), utils.FormatBytes(delta))
			}
			size = vol.SizeBytes - delta
		}
	} else {
		var err error
		size, err = utils.ParseSize(sizeArg)
		if err != nil {
			return err
		}
	}
	resp, err := c.UpdateVolume(ctx, name, v1.VolumeUpdateRequest{SizeBytes: &size})
	if err != nil {
		return wrapErr(err, "volume", name)
	}
	return output(cmd, resp, func() {
		fmt.Printf("volume %q expanded to %s\n", resp.Name, utils.FormatBytes(resp.SizeBytes))
	})
}

func volumeClone(ctx context.Context, cmd *cli.Command) error {
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
}
