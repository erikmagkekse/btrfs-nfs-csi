package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func whoamiCmd() *cli.Command {
	return &cli.Command{
		Name:   "whoami",
		Usage:  "show the authenticated caller's tenant, role, identity, and fingerprint",
		Action: showWhoami,
	}
}

func tokensCmd() *cli.Command {
	return &cli.Command{
		Name:   "tokens",
		Usage:  "list tokens configured for the caller's tenant (admin-only)",
		Flags:  []cli.Flag{watchFlag()},
		Action: watchAction(listTokens),
	}
}

// shortFP truncates a fingerprint to 12 chars for table view; wide keeps the
// full HMAC-SHA256 hex so operators can grep audit logs by full value.
func shortFP(fp string, wide bool) string {
	if wide || len(fp) <= 12 {
		return fp
	}
	return fp[:12]
}

func showWhoami(ctx context.Context, cmd *cli.Command) error {
	resp, err := apiClient.Whoami(ctx)
	if err != nil {
		return err
	}
	return output(cmd, resp, func() {
		fmt.Printf("tenant:      %s\n", resp.Tenant)
		fmt.Printf("role:        %s\n", resp.Role)
		if resp.Identity != "" {
			fmt.Printf("identity:    %s\n", resp.Identity)
		}
		fmt.Printf("fingerprint: %s\n", shortFP(resp.Fingerprint, isWide(cmd)))
	})
}

func listTokens(ctx context.Context, cmd *cli.Command) error {
	resp, err := apiClient.ListTokens(ctx)
	if err != nil {
		return err
	}
	return output(cmd, resp, func() {
		wide := isWide(cmd)
		w := tab()
		_, _ = fmt.Fprintln(w, "ROLE\tIDENTITY\tFINGERPRINT")
		for _, tok := range resp.Tokens {
			id := tok.Identity
			if id == "" {
				id = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", tok.Role, id, shortFP(tok.Fingerprint, wide))
		}
		_ = w.Flush()
	})
}
