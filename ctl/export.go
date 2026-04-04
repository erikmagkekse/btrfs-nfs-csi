package ctl

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func exportAdd(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return fmt.Errorf("usage: export add <volume> <client-ip>")
	}
	vol, client := cmd.Args().Get(0), cmd.Args().Get(1)
	if err := clientFrom(cmd).ExportVolume(ctx, vol, client); err != nil {
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
	if err := clientFrom(cmd).UnexportVolume(ctx, vol, client); err != nil {
		return wrapErr(err, "volume", vol)
	}
	if !isJSON(cmd) {
		fmt.Printf("unexported %q from %s\n", vol, client)
	}
	return nil
}

func exportList(ctx context.Context, cmd *cli.Command) error {
	c := clientFrom(cmd)
	return runWatch(ctx, cmd, func() error {
		resp, err := c.ListExports(ctx)
		if err != nil {
			return err
		}
		return output(cmd, resp, func() {
			tw := newTableWriter(cmd, []string{"PATH", "CLIENT"})
			tw.writeHeader()
			for _, e := range resp.Exports {
				tw.writeRow(map[string]string{"PATH": e.Path, "CLIENT": e.Client})
			}
			tw.flush()
		})
	})
}
