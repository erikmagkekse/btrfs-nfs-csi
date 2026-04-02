package ctl

import (
	"context"
	"fmt"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/urfave/cli/v3"
)

func cloneCmd() *cli.Command {
	return &cli.Command{
		Name:    "clone",
		Aliases: []string{"clones"},
		Usage:   "create clone from snapshot",
		Commands: []*cli.Command{
			{
				Name:      "create",
				Usage:     "create clone from snapshot",
				ArgsUsage: "<snapshot> <name>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 2 {
						return fmt.Errorf("usage: clone create <snapshot> <name>")
					}
					resp, err := clientFrom(cmd).CreateClone(ctx, v1.CloneCreateRequest{Snapshot: cmd.Args().Get(0), Name: cmd.Args().Get(1)})
					if err != nil {
						return wrapErr(err, "clone", cmd.Args().Get(1))
					}
					return output(cmd, resp, func() { fmt.Printf("clone %q created from snapshot %q\n", resp.Name, resp.SourceSnapshot) })
				},
			},
		},
	}
}
