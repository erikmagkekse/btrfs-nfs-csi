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

func listSnapshots(ctx context.Context, cmd *cli.Command, vol, sortBy string, rev bool, opts cliListOpts) error {
	if isWide(cmd) {
		var resp *models.SnapshotDetailListResponse
		var err error
		if vol != "" {
			resp, err = apiClient.ListVolumeSnapshotsDetail(ctx, vol, opts.ListOpts, nil)
		} else {
			resp, err = apiClient.ListSnapshotsDetail(ctx, opts.ListOpts, nil)
		}
		if err != nil {
			return err
		}
		sortSnapshotsDetail(resp.Snapshots, sortBy, rev)
		return output(cmd, resp, func() {
			tw := newTableWriter(cmd, []string{"NAME", "CREATED BY", "VOLUME", "SIZE", "USED", "EXCLUSIVE", "LABELS", "CREATED"})
			tw.writeHeader()
			for _, s := range resp.Snapshots {
				tw.writeRow(map[string]string{
					"NAME": s.Name, "CREATED BY": s.CreatedBy, "VOLUME": s.Volume, "SIZE": utils.FormatBytes(s.SizeBytes), "USED": utils.FormatBytes(s.UsedBytes),
					"EXCLUSIVE": utils.FormatBytes(s.ExclusiveBytes),
					"LABELS":    formatLabelsShort(s.Labels), "CREATED": s.CreatedAt.Local().Format(timeFmt),
				})
			}
			tw.flush()
			emptyHint("snapshots", len(resp.Snapshots), opts.allSet, opts.labelSet)
		})
	}
	var resp *models.SnapshotListResponse
	var err error
	if vol != "" {
		resp, err = apiClient.ListVolumeSnapshots(ctx, vol, opts.ListOpts, nil)
	} else {
		resp, err = apiClient.ListSnapshots(ctx, opts.ListOpts, nil)
	}
	if err != nil {
		return err
	}
	sortSnapshots(resp.Snapshots, sortBy, rev)
	return output(cmd, resp, func() {
		tw := newTableWriter(cmd, []string{"NAME", "CREATED BY", "VOLUME", "SIZE", "USED", "CREATED"})
		tw.writeHeader()
		for _, s := range resp.Snapshots {
			tw.writeRow(map[string]string{
				"NAME": s.Name, "CREATED BY": s.CreatedBy, "VOLUME": s.Volume, "SIZE": utils.FormatBytes(s.SizeBytes),
				"USED": utils.FormatBytes(s.UsedBytes), "CREATED": s.CreatedAt.Local().Format(timeFmt),
			})
		}
		tw.flush()
		emptyHint("snapshots", len(resp.Snapshots), opts.allSet, opts.labelSet)
	})
}

func snapshotGet(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("snapshot name required")
	}
	resp, err := apiClient.GetSnapshot(ctx, name)
	if err != nil {
		return wrapErr(err, "snapshot", name)
	}
	return output(cmd, resp, func() {
		fmt.Printf("Name:       %s\n", resp.Name)
		fmt.Printf("Volume:     %s\n", resp.Volume)
		fmt.Printf("Size:       %s\n", utils.FormatBytes(resp.SizeBytes))
		fmt.Printf("Used:       %s\n", utils.FormatBytes(resp.UsedBytes))
		fmt.Printf("Exclusive:  %s\n", utils.FormatBytes(resp.ExclusiveBytes))
		printLabels("Labels:", resp.Labels, 12)
		fmt.Printf("Created:    %s\n", resp.CreatedAt.Local().Format(timeFmt))
		fmt.Printf("Updated:    %s\n", resp.UpdatedAt.Local().Format(timeFmt))
	})
}

func snapshotCreate(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: snapshot create <volume> <name>")
	}
	resp, err := apiClient.CreateSnapshot(ctx, models.SnapshotCreateRequest{Volume: cmd.Args().Get(0), Name: cmd.Args().Get(1), Labels: parseLabelsFlag(cmd)})
	if err != nil {
		return wrapErr(err, "snapshot", cmd.Args().Get(1))
	}
	return output(cmd, resp, func() { fmt.Printf("snapshot %q created from volume %q\n", resp.Name, resp.Volume) })
}

func snapshotDelete(ctx context.Context, cmd *cli.Command) error {
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return fmt.Errorf("snapshot name required")
	}
	force := os.Getenv("BTRFS_NFS_CSI_FORCE") == "true"
	confirmed := force || (cmd.Bool("confirm") && cmd.Bool("yes"))
	var protected []string
	for _, name := range names {
		if !confirmed {
			snap, err := apiClient.GetSnapshot(ctx, name)
			if err != nil {
				return wrapErr(err, "snapshot", name)
			}
			if snap.Labels[config.LabelCreatedBy] != apiClient.Identity() {
				owner := snap.Labels[config.LabelCreatedBy]
				if owner == "" {
					owner = "unknown"
				}
				_, _ = fmt.Fprintf(os.Stderr, "skipped %q (created-by: %s)\n", name, owner)
				protected = append(protected, name)
				continue
			}
		}
		if err := apiClient.DeleteSnapshot(ctx, name); err != nil {
			return wrapErr(err, "snapshot", name)
		}
		if !isJSON(cmd) {
			fmt.Printf("snapshot %q deleted\n", name)
		}
	}
	if len(protected) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "to force:  btrfs-nfs-csi snapshot delete %s --confirm --yes\n", strings.Join(protected, " "))
	}
	return nil
}

func snapshotClone(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: snapshot clone <snapshot> <name>")
	}
	resp, err := apiClient.CreateClone(ctx, models.CloneCreateRequest{Snapshot: cmd.Args().Get(0), Name: cmd.Args().Get(1), Labels: parseLabelsFlag(cmd)})
	if err != nil {
		return wrapErr(err, "clone", cmd.Args().Get(1))
	}
	return output(cmd, resp, func() { fmt.Printf("clone %q created from snapshot %q\n", resp.Name, cmd.Args().Get(0)) })
}
