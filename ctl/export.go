package ctl

import (
	"context"
	"fmt"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/urfave/cli/v3"
)

func exportAdd(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: export add <volume> <client-ip>")
	}
	vol, client := cmd.Args().Get(0), cmd.Args().Get(1)
	labels := parseLabelsFlag(cmd)
	if err := apiClient.CreateVolumeExport(ctx, vol, client, labels); err != nil {
		return wrapErr(err, "volume", vol)
	}
	if !isJSON(cmd) {
		fmt.Printf("exported %q to %s\n", vol, client)
	}
	return nil
}

func exportRemove(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: export remove <volume> <client-ip>")
	}
	vol, client := cmd.Args().Get(0), cmd.Args().Get(1)
	labels := parseLabelsFlag(cmd)
	if err := apiClient.DeleteVolumeExport(ctx, vol, client, labels); err != nil {
		return wrapErr(err, "volume", vol)
	}
	if !isJSON(cmd) {
		fmt.Printf("unexported %q from %s\n", vol, client)
	}
	return nil
}

func listExports(ctx context.Context, cmd *cli.Command, sortBy string, rev bool, opts v1.ListOpts) error {
	if isWide(cmd) {
		resp, err := apiClient.ListVolumeExportsDetail(ctx, opts)
		if err != nil {
			return err
		}
		sortExportsDetailList(resp.Exports, sortBy, rev)
		return output(cmd, resp, func() {
			tw := newTableWriter(cmd, []string{"NAME", "CLIENT", "LABELS", "CREATED"})
			tw.writeHeader()
			for _, e := range resp.Exports {
				created := ""
				if !e.CreatedAt.IsZero() {
					created = e.CreatedAt.Format(timeFmt)
				}
				tw.writeRow(map[string]string{
					"NAME":    e.Name,
					"CLIENT":  e.Client,
					"LABELS":  formatLabelsShort(e.Labels),
					"CREATED": created,
				})
			}
			tw.flush()
		})
	}
	resp, err := apiClient.ListVolumeExports(ctx, opts)
	if err != nil {
		return err
	}
	sortExportsList(resp.Exports, sortBy, rev)
	return output(cmd, resp, func() {
		tw := newTableWriter(cmd, []string{"NAME", "CLIENT", "CREATED"})
		tw.writeHeader()
		for _, e := range resp.Exports {
			created := ""
			if !e.CreatedAt.IsZero() {
				created = e.CreatedAt.Format(timeFmt)
			}
			tw.writeRow(map[string]string{
				"NAME":    e.Name,
				"CLIENT":  e.Client,
				"CREATED": created,
			})
		}
		tw.flush()
	})
}
