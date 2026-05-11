package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	agentclient "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/client"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/urfave/cli/v3"
)

// Tenant/Role/Fingerprint are cached so `agents ls` works offline. They
// are deterministic from the token, so the cache only goes stale on a
// fresh `login` with a different token, never spontaneously.
type Agent struct {
	URL           string            `json:"url"`
	Token         string            `json:"token"`
	Identity      string            `json:"identity,omitempty"`
	Tenant        string            `json:"tenant,omitempty"`
	Role          models.TenantRole `json:"role,omitempty"`
	Fingerprint   string            `json:"fingerprint,omitempty"`
	TLSSkipVerify bool              `json:"tls_skip_verify,omitempty"`
}

type AgentStore struct {
	Current string           `json:"current,omitempty"`
	Agents  map[string]Agent `json:"agents,omitempty"`
}

func (s *AgentStore) Active() (Agent, bool) {
	if s == nil || s.Current == "" {
		return Agent{}, false
	}
	a, ok := s.Agents[s.Current]
	return a, ok
}

// agentsPath honours BTRFS_NFS_CSI_AGENTS_FILE so tests and per-project
// profiles can redirect away from the default $HOME location.
func agentsPath() (string, error) {
	if override := os.Getenv("BTRFS_NFS_CSI_AGENTS_FILE"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".btrfs-nfs-csi", "agents.json"), nil
}

// loadAgents treats a missing file as an empty store so first-run is
// indistinguishable from "no agents configured".
func loadAgents() (*AgentStore, error) {
	path, err := agentsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &AgentStore{Agents: map[string]Agent{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s AgentStore
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.Agents == nil {
		s.Agents = map[string]Agent{}
	}
	return &s, nil
}

// save writes atomically with 0600 + 0700 because the file holds bearer
// tokens in plaintext.
func (s *AgentStore) save() error {
	path, err := agentsPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agents: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "agents-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	// rename is atomic, but without fsync the new bytes can be lost on
	// power loss while the directory entry already points at them.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	cleanup = false
	return nil
}

// newAgentClient layers per-agent settings on top of AGENT_HTTP_CLIENT_*
// env defaults. A saved skip=true wins because operators store it
// deliberately, env defaults fill the rest.
func newAgentClient(a Agent) (*agentclient.Client, error) {
	cfg := agentclient.DefaultClientConfig()
	if a.TLSSkipVerify {
		cfg.TLSSkipVerify = true
	}
	if cfg.Identity == "" {
		cfg.Identity = a.Identity
	}
	return agentclient.NewClientWithConfig(a.URL, a.Token, cfg)
}

func agentsSubcommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "login",
			Usage:     "save an agent endpoint and make it active",
			ArgsUsage: "<name>",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "url", Usage: "agent API URL"},
				&cli.StringFlag{Name: "token", Usage: "tenant token; prefer omitting and entering at the no-echo prompt so it does not land in shell history"},
				&cli.StringFlag{Name: "identity", Usage: "identity label value", Value: config.IdentityCLI},
				&cli.BoolFlag{Name: "tls-skip-verify", Usage: "skip TLS certificate verification for this agent (use with self-signed certs)"},
			},
			Action: agentLogin,
		},
		{
			Name:      "logout",
			Usage:     "remove a saved agent (defaults to the active one)",
			ArgsUsage: "[<name>]",
			Action:    agentLogout,
		},
		{
			Name:    "ls",
			Aliases: []string{"list"},
			Usage:   "list saved agents (* marks the active one)",
			Flags:   []cli.Flag{outputFlag()},
			Action:  agentList,
		},
		{
			Name:      "use",
			Usage:     "switch to a saved agent",
			ArgsUsage: "<name>",
			Action:    agentUse,
		},
		{
			Name:      "verify",
			Usage:     "check that saved tokens still authenticate and match the cached tenant/role/fingerprint",
			ArgsUsage: "[<name>]",
			Flags: []cli.Flag{
				&cli.BoolFlag{Name: "all", Usage: "verify every saved agent in parallel"},
				outputFlag(),
			},
			Action: agentVerify,
		},
	}
}

func agentLogin(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errors.New("missing agent name (usage: btrfs-nfs-csi agents login <name> --url ...)")
	}
	url := cmd.String("url")
	if url == "" {
		return errors.New("--url is required")
	}
	identity := cmd.String("identity")
	tlsSkipVerify := cmd.Bool("tls-skip-verify")
	token := cmd.String("token")
	if token == "" {
		t, err := readToken()
		if err != nil {
			return err
		}
		if t == "" {
			return errors.New("empty token")
		}
		token = t
	}

	candidate := Agent{URL: url, Token: token, Identity: identity, TLSSkipVerify: tlsSkipVerify}
	client, err := newAgentClient(candidate)
	if err != nil {
		return fmt.Errorf("client init: %w", err)
	}
	if err := client.Resolve(ctx); err != nil {
		return fmt.Errorf("verify against %s: %w", url, err)
	}

	store, err := loadAgents()
	if err != nil {
		return err
	}
	who := client.Whoami()
	store.Agents[name] = Agent{
		URL:           url,
		Token:         token,
		Identity:      identity,
		TLSSkipVerify: tlsSkipVerify,
		Tenant:        who.Tenant,
		Role:          who.Role,
		Fingerprint:   who.Fingerprint,
	}
	store.Current = name
	if err := store.save(); err != nil {
		return err
	}
	fmt.Printf("logged in to %s as tenant %q (role=%s, identity=%s)\n", url, who.Tenant, who.Role, identity)
	fmt.Printf("agent %q is now active\n", name)
	return nil
}

func agentLogout(_ context.Context, cmd *cli.Command) error {
	store, err := loadAgents()
	if err != nil {
		return err
	}
	name := cmd.Args().First()
	if name == "" {
		name = store.Current
	}
	if name == "" {
		return errors.New("no active agent and no name given")
	}
	if _, ok := store.Agents[name]; !ok {
		return fmt.Errorf("agent %q not found", name)
	}
	delete(store.Agents, name)
	if store.Current == name {
		store.Current = ""
	}
	if err := store.save(); err != nil {
		return err
	}
	fmt.Printf("removed agent %q\n", name)
	if store.Current == "" && len(store.Agents) > 0 {
		fmt.Fprintln(os.Stderr, "warning: no active agent, pick one with `btrfs-nfs-csi agents use <name>`")
	}
	return nil
}

// agentListEntry deliberately omits the token so `-o json` is safe to
// redirect or pipe.
type agentListEntry struct {
	Name          string            `json:"name"`
	Active        bool              `json:"active,omitempty"`
	URL           string            `json:"url"`
	Identity      string            `json:"identity,omitempty"`
	Tenant        string            `json:"tenant,omitempty"`
	Role          models.TenantRole `json:"role,omitempty"`
	Fingerprint   string            `json:"fingerprint,omitempty"`
	TLSSkipVerify bool              `json:"tls_skip_verify,omitempty"`
}

type agentListOutput struct {
	Current string           `json:"current,omitempty"`
	Agents  []agentListEntry `json:"agents"`
}

func agentList(_ context.Context, cmd *cli.Command) error {
	store, err := loadAgents()
	if err != nil {
		return err
	}
	if len(store.Agents) == 0 {
		if isJSON(cmd) {
			return output(cmd, agentListOutput{Agents: []agentListEntry{}}, nil)
		}
		fmt.Fprintln(os.Stderr, "no agents configured (run `btrfs-nfs-csi agents login <name> --url ...`)")
		return nil
	}

	names := slices.Sorted(maps.Keys(store.Agents))
	entries := make([]agentListEntry, len(names))
	for i, name := range names {
		a := store.Agents[name]
		entries[i] = agentListEntry{
			Name:          name,
			Active:        name == store.Current,
			URL:           a.URL,
			Identity:      a.Identity,
			Tenant:        a.Tenant,
			Role:          a.Role,
			Fingerprint:   a.Fingerprint,
			TLSSkipVerify: a.TLSSkipVerify,
		}
	}

	return output(cmd, agentListOutput{Current: store.Current, Agents: entries}, func() {
		wide := isWide(cmd)
		w := tab()
		if wide {
			_, _ = fmt.Fprintln(w, "ACTIVE\tNAME\tURL\tTENANT\tROLE\tIDENTITY\tFINGERPRINT\tTLS")
		} else {
			_, _ = fmt.Fprintln(w, "ACTIVE\tNAME\tURL\tTENANT\tROLE\tIDENTITY\tFINGERPRINT")
		}
		for _, e := range entries {
			marker := ""
			if e.Active {
				marker = "*"
			}
			if wide {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					marker, e.Name, e.URL, dash(e.Tenant), dash(string(e.Role)), dash(e.Identity), shortFP(e.Fingerprint, wide), tlsLabel(e.TLSSkipVerify))
			} else {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					marker, e.Name, e.URL, dash(e.Tenant), dash(string(e.Role)), dash(e.Identity), shortFP(e.Fingerprint, wide))
			}
		}
		_ = w.Flush()
	})
}

func tlsLabel(skip bool) string {
	if skip {
		return "skip"
	}
	return "verify"
}

func agentUse(_ context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errors.New("missing agent name (usage: btrfs-nfs-csi agents use <name>)")
	}
	store, err := loadAgents()
	if err != nil {
		return err
	}
	if _, ok := store.Agents[name]; !ok {
		return fmt.Errorf("agent %q not found", name)
	}
	store.Current = name
	if err := store.save(); err != nil {
		return err
	}
	fmt.Printf("switched to agent %q\n", name)
	return nil
}

// verifyStatus marshals lowercase for JSON. The table view uppercases the
// non-ok cases via displayStatus so failures catch the eye.
type verifyStatus string

const (
	statusOk    verifyStatus = "ok"
	statusStale verifyStatus = "stale"
	statusFail  verifyStatus = "fail"
)

type verifyEntry struct {
	Name        string            `json:"name"`
	Active      bool              `json:"active,omitempty"`
	Status      verifyStatus      `json:"status"`
	Tenant      string            `json:"tenant,omitempty"`
	Role        models.TenantRole `json:"role,omitempty"`
	Fingerprint string            `json:"fingerprint,omitempty"`
	Drift       string            `json:"drift,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type verifyOutput struct {
	Results []verifyEntry `json:"results"`
}

func agentVerify(ctx context.Context, cmd *cli.Command) error {
	store, err := loadAgents()
	if err != nil {
		return err
	}

	var names []string
	switch {
	case cmd.Bool("all"):
		if len(store.Agents) == 0 {
			if isJSON(cmd) {
				return output(cmd, verifyOutput{Results: []verifyEntry{}}, nil)
			}
			fmt.Fprintln(os.Stderr, "no agents configured")
			return nil
		}
		names = slices.Sorted(maps.Keys(store.Agents))
	default:
		name := cmd.Args().First()
		if name == "" {
			name = store.Current
		}
		if name == "" {
			return errors.New("no active agent and no name given (use --all to verify every saved agent)")
		}
		if _, ok := store.Agents[name]; !ok {
			return fmt.Errorf("agent %q not found", name)
		}
		names = []string{name}
	}

	entries := make([]verifyEntry, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			entries[i] = checkAgent(ctx, name, store.Agents[name], name == store.Current)
		})
	}
	wg.Wait()

	failed := false
	for _, e := range entries {
		if e.Status != statusOk {
			failed = true
			break
		}
	}

	if err := output(cmd, verifyOutput{Results: entries}, func() {
		w := tab()
		_, _ = fmt.Fprintln(w, "ACTIVE\tNAME\tSTATUS\tNOTE")
		for _, e := range entries {
			marker := ""
			if e.Active {
				marker = "*"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", marker, e.Name, displayStatus(e.Status), verifyNote(e))
		}
		_ = w.Flush()
	}); err != nil {
		return err
	}
	if failed {
		return errors.New("one or more agents failed verification")
	}
	return nil
}

func checkAgent(ctx context.Context, name string, a Agent, active bool) verifyEntry {
	entry := verifyEntry{Name: name, Active: active}
	client, err := newAgentClient(a)
	if err != nil {
		entry.Status, entry.Error = statusFail, err.Error()
		return entry
	}
	if err := client.Resolve(ctx); err != nil {
		entry.Status, entry.Error = statusFail, err.Error()
		return entry
	}
	who := client.Whoami()
	entry.Tenant, entry.Role, entry.Fingerprint = who.Tenant, who.Role, who.Fingerprint
	if d := diffAgent(a, who); d != "" {
		entry.Status, entry.Drift = statusStale, d
		return entry
	}
	entry.Status = statusOk
	return entry
}

func diffAgent(a Agent, who *models.WhoamiResponse) string {
	var diffs []string
	if a.Tenant != who.Tenant {
		diffs = append(diffs, fmt.Sprintf("tenant: cached=%s, live=%s", a.Tenant, who.Tenant))
	}
	if a.Role != who.Role {
		diffs = append(diffs, fmt.Sprintf("role: cached=%s, live=%s", a.Role, who.Role))
	}
	if a.Fingerprint != who.Fingerprint {
		diffs = append(diffs, fmt.Sprintf("fingerprint: cached=%s, live=%s", shortFP(a.Fingerprint, false), shortFP(who.Fingerprint, false)))
	}
	return strings.Join(diffs, "; ")
}

func displayStatus(s verifyStatus) string {
	if s == statusOk {
		return string(s)
	}
	return strings.ToUpper(string(s))
}

func verifyNote(e verifyEntry) string {
	switch e.Status {
	case statusFail:
		return e.Error
	case statusStale:
		return e.Drift + " (run `agents login <name>` to refresh)"
	default:
		return ""
	}
}

