package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
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

func hashTokenCmd() *cli.Command {
	return &cli.Command{
		Name:  "hash-token",
		Usage: "hash a token with bcrypt for use in AGENT_TENANTS",
		Description: `Reads a token from stdin (no echo if interactive) and prints its bcrypt
hash. Drop the printed value into AGENT_TENANTS in place of the plaintext;
clients still send the original value as bearer.`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "cost",
				Usage: "bcrypt cost (4-31; higher is slower and stronger)",
				Value: 12,
			},
		},
		Action: hashToken,
	}
}

func hashToken(_ context.Context, cmd *cli.Command) error {
	cost := cmd.Int("cost")
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return fmt.Errorf("cost %d out of range (%d-%d)", cost, bcrypt.MinCost, bcrypt.MaxCost)
	}

	token, err := readToken()
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("empty token")
	}

	h, err := bcrypt.GenerateFromPassword([]byte(token), int(cost))
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}
	fmt.Println(string(h))
	return nil
}

// readToken returns the token from stdin: no-echo prompt when interactive,
// trimmed first line otherwise (so `echo s3cret | ... hash-token` works in scripts).
func readToken() (string, error) {
	stdin := int(os.Stdin.Fd())
	if term.IsTerminal(stdin) {
		fmt.Fprint(os.Stderr, "Token: ")
		b, err := term.ReadPassword(stdin)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
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
	resp := apiClient.Whoami()
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
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", tok.Role, dash(tok.Identity), shortFP(tok.Fingerprint, wide))
		}
		_ = w.Flush()
	})
}
