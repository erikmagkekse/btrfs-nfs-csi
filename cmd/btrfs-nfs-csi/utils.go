package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	agentclient "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/client"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	outputTable = "table"
	outputWide  = "wide"
	outputJSON  = "json"
	timeFmt     = "2006-01-02 15:04"

	sortSize    = "size"
	sortUsed    = "used"
	sortUsedPct = "used%"
	sortCreated = "created"
	sortExports = "clients"
	sortVolume  = "volume"
	sortType    = "type"
	sortStatus  = "status"
)

func printLabels(header string, labels map[string]string, indent int) {
	if len(labels) == 0 {
		fmt.Printf("%-*s%s\n", indent, header, "none")
		return
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for i, k := range keys {
		if i == 0 {
			fmt.Printf("%-*s%s=%s\n", indent, header, k, labels[k])
		} else {
			fmt.Printf("%-*s%s=%s\n", indent, "", k, labels[k])
		}
	}
}

func formatLabelsShort(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(labels))
	if v, ok := labels[config.LabelCreatedBy]; ok {
		parts = append(parts, config.LabelCreatedBy+"="+v)
	}
	rest := make([]string, 0, len(labels))
	for k, v := range labels {
		if k == config.LabelCreatedBy {
			continue
		}
		rest = append(rest, k+"="+v)
	}
	slices.Sort(rest)
	parts = append(parts, rest...)
	s := strings.Join(parts, ", ")
	if len(s) > 48 {
		return s[:45] + "..."
	}
	return s
}

func formatExports(refs []models.ExportDetailResponse) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Client
		if len(r.Labels) > 0 {
			s += " (" + formatLabelsShort(r.Labels) + ")"
		}
		if !r.CreatedAt.IsZero() {
			s += " since " + r.CreatedAt.Local().Format(timeFmt)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func runWatch(ctx context.Context, cmd *cli.Command, fn func() error) error {
	if !cmd.IsSet("watch") || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn()
	}
	ranWatch = true
	interval := cmd.Duration("watch")
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if termios, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS); err == nil {
		noEcho := *termios
		noEcho.Lflag &^= unix.ECHO
		_ = unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, &noEcho)
		defer func() { _ = unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, termios) }()
	}
	fmt.Print("\033[?1049h\033[?25l")
	defer fmt.Print("\033[?25h\033[?1049l")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		fmt.Print("\033[H")
		start := time.Now()
		if err := fn(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n%s, refresh %s\n", fmtTiming(time.Since(start)), interval)
		fmt.Print("\033[J")
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

type tableWriter struct {
	selected []string
	w        *tabwriter.Writer
}

func newTableWriter(cmd *cli.Command, all []string) *tableWriter {
	selected := all
	if raw := cmd.String("columns"); raw != "" {
		avail := make(map[string]string, len(all))
		for _, col := range all {
			key := strings.ToLower(strings.ReplaceAll(col, " ", ""))
			avail[key] = col
		}
		var filtered []string
		for r := range strings.SplitSeq(raw, ",") {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(r), " ", ""))
			if col, ok := avail[key]; ok {
				filtered = append(filtered, col)
			}
		}
		if len(filtered) > 0 {
			selected = filtered
		}
	}
	tw := &tableWriter{selected: selected}
	if len(selected) > 1 {
		tw.w = tab()
	}
	return tw
}

func (tw *tableWriter) writeHeader() {
	if tw.w == nil {
		return
	}
	_, _ = fmt.Fprintln(tw.w, strings.Join(tw.selected, "\t"))
}

func (tw *tableWriter) writeRow(values map[string]string) {
	if tw.w == nil {
		fmt.Println(values[tw.selected[0]])
		return
	}
	parts := make([]string, 0, len(tw.selected))
	for _, col := range tw.selected {
		parts = append(parts, values[col])
	}
	_, _ = fmt.Fprintln(tw.w, strings.Join(parts, "\t"))
}

func (tw *tableWriter) flush() {
	if tw.w != nil {
		_ = tw.w.Flush()
	}
}

func sortVolumes(vols []models.VolumeResponse, field string, reverse bool) {
	slices.SortFunc(vols, func(a, b models.VolumeResponse) int {
		var c int
		switch field {
		case sortSize:
			c = cmp.Compare(a.SizeBytes, b.SizeBytes)
		case sortUsed:
			c = cmp.Compare(a.UsedBytes, b.UsedBytes)
		case sortUsedPct:
			c = cmp.Compare(usedPct(a.UsedBytes, a.SizeBytes), usedPct(b.UsedBytes, b.SizeBytes))
		case sortCreated:
			c = a.CreatedAt.Compare(b.CreatedAt)
		case sortExports:
			c = cmp.Compare(a.Exports, b.Exports)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortVolumesDetail(vols []models.VolumeDetailResponse, field string, reverse bool) {
	slices.SortFunc(vols, func(a, b models.VolumeDetailResponse) int {
		var c int
		switch field {
		case sortSize:
			c = cmp.Compare(a.SizeBytes, b.SizeBytes)
		case sortUsed:
			c = cmp.Compare(a.UsedBytes, b.UsedBytes)
		case sortUsedPct:
			c = cmp.Compare(usedPct(a.UsedBytes, a.SizeBytes), usedPct(b.UsedBytes, b.SizeBytes))
		case sortCreated:
			c = a.CreatedAt.Compare(b.CreatedAt)
		case sortExports:
			c = cmp.Compare(len(a.Exports), len(b.Exports))
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortSnapshots(snaps []models.SnapshotResponse, field string, reverse bool) {
	slices.SortFunc(snaps, func(a, b models.SnapshotResponse) int {
		var c int
		switch field {
		case sortSize:
			c = cmp.Compare(a.SizeBytes, b.SizeBytes)
		case sortUsed:
			c = cmp.Compare(a.UsedBytes, b.UsedBytes)
		case sortCreated:
			c = a.CreatedAt.Compare(b.CreatedAt)
		case sortVolume:
			c = cmp.Compare(a.Volume, b.Volume)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortSnapshotsDetail(snaps []models.SnapshotDetailResponse, field string, reverse bool) {
	slices.SortFunc(snaps, func(a, b models.SnapshotDetailResponse) int {
		var c int
		switch field {
		case sortSize:
			c = cmp.Compare(a.SizeBytes, b.SizeBytes)
		case sortUsed:
			c = cmp.Compare(a.UsedBytes, b.UsedBytes)
		case sortCreated:
			c = a.CreatedAt.Compare(b.CreatedAt)
		case sortVolume:
			c = cmp.Compare(a.Volume, b.Volume)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortExportsList(exports []models.ExportResponse, field string, reverse bool) {
	slices.SortFunc(exports, func(a, b models.ExportResponse) int {
		var c int
		switch field {
		case "client":
			c = cmp.Compare(a.Client, b.Client)
		case sortCreated:
			c = a.CreatedAt.Compare(b.CreatedAt)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortExportsDetailList(exports []models.ExportDetailResponse, field string, reverse bool) {
	slices.SortFunc(exports, func(a, b models.ExportDetailResponse) int {
		var c int
		switch field {
		case "client":
			c = cmp.Compare(a.Client, b.Client)
		case sortCreated:
			c = a.CreatedAt.Compare(b.CreatedAt)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortTasks(tasks []models.TaskResponse, field string, reverse bool) {
	slices.SortFunc(tasks, func(a, b models.TaskResponse) int {
		var c int
		switch field {
		case sortType:
			c = cmp.Compare(a.Type, b.Type)
		case sortStatus:
			c = cmp.Compare(a.Status, b.Status)
		default:
			c = a.CreatedAt.Compare(b.CreatedAt)
		}
		if reverse {
			return -c
		}
		return c
	})
}

func sortTasksDetail(tasks []models.TaskDetailResponse, field string, reverse bool) {
	slices.SortFunc(tasks, func(a, b models.TaskDetailResponse) int {
		var c int
		switch field {
		case sortType:
			c = cmp.Compare(a.Type, b.Type)
		case sortStatus:
			c = cmp.Compare(a.Status, b.Status)
		default:
			c = a.CreatedAt.Compare(b.CreatedAt)
		}
		if reverse {
			return -c
		}
		return c
	})
}

var (
	apiClient  *agentclient.Client
	cmdStart   time.Time
	ranWatch   bool
	timingLine string
)

func initClient(cmd *cli.Command) error {
	var err error
	apiClient, err = agentclient.NewClient(cmd.String("agent-url"), cmd.String("agent-token"), config.IdentityCLI)
	return err
}

func withCLIHooks(cmds ...*cli.Command) []*cli.Command {
	for _, cmd := range cmds {
		cmd.Flags = append(cmd.Flags,
			&cli.StringFlag{Name: "agent-url", Sources: cli.EnvVars("AGENT_URL"), Usage: "agent API URL"},
			&cli.StringFlag{Name: "agent-token", Sources: cli.EnvVars("AGENT_TOKEN"), Usage: "tenant token"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: outputTable, Usage: "output format: table, wide, json, json,wide"},
		)
		cmd.Before = func(ctx context.Context, c *cli.Command) (context.Context, error) {
			if err := initClient(c); err != nil {
				return ctx, err
			}
			cmdStart = time.Now()
			return ctx, nil
		}
		cmd.After = func(ctx context.Context, c *cli.Command) error {
			if !cmdStart.IsZero() && !ranWatch && !isJSON(c) && c.String("columns") == "" {
				timingLine = fmtTiming(time.Since(cmdStart))
			}
			return nil
		}
	}
	return cmds
}

func fmtTiming(d time.Duration) string {
	return fmt.Sprintf("took %s, %s", d.Truncate(time.Millisecond), time.Now().Format("2006-01-02 15:04:05"))
}

func watchAction(fn cli.ActionFunc) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		return runWatch(ctx, cmd, func() error {
			return fn(ctx, cmd)
		})
	}
}

func isWide(cmd *cli.Command) bool {
	o := cmd.String("output")
	return o == outputWide || strings.Contains(o, "wide")
}

func isJSON(cmd *cli.Command) bool {
	return strings.Contains(cmd.String("output"), outputJSON)
}

func output(cmd *cli.Command, data any, tableFn func()) error {
	if isJSON(cmd) {
		printJSON(data)
		return nil
	}
	tableFn()
	return nil
}

func tab() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func wrapErr(err error, resource, name string) error {
	if err == nil {
		return nil
	}
	switch {
	case models.IsNotFound(err):
		return fmt.Errorf("%s %q not found", resource, name)
	case models.IsConflict(err):
		return fmt.Errorf("%s %q already exists", resource, name)
	case models.IsLocked(err):
		return fmt.Errorf("%s %q is busy (active exports?)", resource, name)
	default:
		return err
	}
}

func usedPct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}
