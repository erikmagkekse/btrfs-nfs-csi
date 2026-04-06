package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/urfave/cli/v3"
)

func listVolumes(ctx context.Context, cmd *cli.Command, sortBy string, rev bool, opts cliListOpts) error {
	if isWide(cmd) {
		resp, err := apiClient.ListVolumesDetail(ctx, opts.ListOpts, nil)
		if err != nil {
			return err
		}
		sortVolumesDetail(resp.Volumes, sortBy, rev)
		return output(cmd, resp, func() {
			tw := newTableWriter(cmd, []string{"NAME", "CREATED BY", "SIZE", "USED", "QUOTA", "COMPRESSION", "NOCOW", "UID", "GID", "MODE", "LABELS", "CLIENTS", "CREATED"})
			tw.writeHeader()
			for _, v := range resp.Volumes {
				tw.writeRow(map[string]string{
					"NAME": v.Name, "CREATED BY": v.CreatedBy, "SIZE": utils.FormatBytes(v.SizeBytes), "USED": utils.FormatBytes(v.UsedBytes),
					"QUOTA": utils.FormatBytes(v.QuotaBytes), "COMPRESSION": v.Compression, "NOCOW": fmt.Sprintf("%v", v.NoCOW),
					"UID": fmt.Sprintf("%d", v.UID), "GID": fmt.Sprintf("%d", v.GID), "MODE": v.Mode,
					"LABELS": formatLabelsShort(v.Labels), "CLIENTS": fmt.Sprintf("%d", len(v.Exports)), "CREATED": v.CreatedAt.Local().Format(timeFmt),
				})
			}
			tw.flush()
			emptyHint("volumes", len(resp.Volumes), opts.allSet, opts.labelSet)
		})
	}
	resp, err := apiClient.ListVolumes(ctx, opts.ListOpts, nil)
	if err != nil {
		return err
	}
	sortVolumes(resp.Volumes, sortBy, rev)
	return output(cmd, resp, func() {
		tw := newTableWriter(cmd, []string{"NAME", "CREATED BY", "SIZE", "USED", "USED%", "CLIENTS", "CREATED"})
		tw.writeHeader()
		for _, v := range resp.Volumes {
			tw.writeRow(map[string]string{
				"NAME": v.Name, "CREATED BY": v.CreatedBy, "SIZE": utils.FormatBytes(v.SizeBytes), "USED": utils.FormatBytes(v.UsedBytes),
				"USED%":   fmt.Sprintf("%.0f%%", usedPct(v.UsedBytes, v.SizeBytes)),
				"CLIENTS": fmt.Sprintf("%d", v.Exports), "CREATED": v.CreatedAt.Local().Format(timeFmt),
			})
		}
		tw.flush()
		emptyHint("volumes", len(resp.Volumes), opts.allSet, opts.labelSet)
	})
}

func volumeGet(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("volume name required")
	}
	resp, err := apiClient.GetVolume(ctx, name)
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
		fmt.Printf("Created:      %s\n", resp.CreatedAt.Local().Format(timeFmt))
		fmt.Printf("Updated:      %s\n", resp.UpdatedAt.Local().Format(timeFmt))
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
	req := models.VolumeCreateRequest{
		Name:        cmd.Args().Get(0),
		SizeBytes:   size,
		Compression: compression,
		NoCOW:       cmd.Bool("nocow"),
		UID:         int(cmd.Int("uid")),
		GID:         int(cmd.Int("gid")),
		Mode:        cmd.String("mode"),
		Labels:      parseLabelsFlag(cmd),
	}
	resp, err := apiClient.CreateVolume(ctx, req)
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
	var protected []string
	for _, name := range names {
		if !confirmed {
			vol, err := apiClient.GetVolume(ctx, name)
			if err != nil {
				return wrapErr(err, "volume", name)
			}
			if vol.Labels[config.LabelCreatedBy] != apiClient.Identity() {
				owner := vol.Labels[config.LabelCreatedBy]
				if owner == "" {
					owner = "unknown"
				}
				_, _ = fmt.Fprintf(os.Stderr, "skipped %q (created-by: %s)\n", name, owner)
				protected = append(protected, name)
				continue
			}
		}
		if err := apiClient.DeleteVolume(ctx, name); err != nil {
			return wrapErr(err, "volume", name)
		}
		if !isJSON(cmd) {
			fmt.Printf("volume %q deleted\n", name)
		}
	}
	if len(protected) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "to force:  btrfs-nfs-csi volume delete %s --confirm --yes\n", strings.Join(protected, " "))
	}
	return nil
}

func volumeExpand(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: volume expand <name> <size|+size>")
	}
	name := cmd.Args().Get(0)
	sizeArg := cmd.Args().Get(1)
	var size uint64
	if (sizeArg[0] == '+' || sizeArg[0] == '-') && len(sizeArg) > 1 && sizeArg[1] >= '0' && sizeArg[1] <= '9' {
		delta, err := utils.ParseSize(sizeArg[1:])
		if err != nil {
			return err
		}
		vol, err := apiClient.GetVolume(ctx, name)
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
	resp, err := apiClient.UpdateVolume(ctx, name, models.VolumeUpdateRequest{SizeBytes: &size})
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
	resp, err := apiClient.CloneVolume(ctx, models.VolumeCloneRequest{Source: cmd.Args().Get(0), Name: cmd.Args().Get(1), Labels: parseLabelsFlag(cmd)})
	if err != nil {
		return wrapErr(err, "volume", cmd.Args().Get(1))
	}
	return output(cmd, resp, func() {
		fmt.Printf("volume %q cloned from %q\n", resp.Name, cmd.Args().Get(0))
	})
}
