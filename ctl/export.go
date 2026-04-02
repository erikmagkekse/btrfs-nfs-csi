package ctl

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

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
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: export add <volume> <client-ip>")
					}
					vol, client := cmd.Args().Get(0), cmd.Args().Get(1)
					if err := clientFrom(cmd).ExportVolume(ctx, vol, client); err != nil {
						return wrapErr(err, "volume", vol)
					}
					return output(cmd, map[string]string{"volume": vol, "client": client}, func() {
						fmt.Printf("exported %q to %s\n", vol, client)
					})
				},
			},
			{
				Name:      "remove",
				Aliases:   []string{"rm"},
				Usage:     "remove NFS export",
				ArgsUsage: "<volume> <client-ip>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: export remove <volume> <client-ip>")
					}
					vol, client := cmd.Args().Get(0), cmd.Args().Get(1)
					if err := clientFrom(cmd).UnexportVolume(ctx, vol, client); err != nil {
						return wrapErr(err, "volume", vol)
					}
					return output(cmd, map[string]string{"volume": vol, "client": client}, func() {
						fmt.Printf("unexported %q from %s\n", vol, client)
					})
				},
			},
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list active NFS exports",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					resp, err := clientFrom(cmd).ListExports(ctx)
					if err != nil {
						return err
					}
					return output(cmd, resp, func() {
						w := tab()
						_, _ = fmt.Fprintln(w, "PATH\tCLIENT")
						for _, e := range resp.Exports {
							_, _ = fmt.Fprintf(w, "%s\t%s\n", e.Path, e.Client)
						}
						_ = w.Flush()
					})
				},
			},
		},
	}
}
