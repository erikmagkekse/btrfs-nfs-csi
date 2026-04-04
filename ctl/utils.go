package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
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

	sortName    = "name"
	sortSize    = "size"
	sortUsed    = "used"
	sortUsedPct = "used%"
	sortCreated = "created"
	sortExports = "clients"
	sortVolume  = "volume"
)

func labelFlag() cli.Flag {
	return &cli.StringSliceFlag{Name: "label", Aliases: []string{"l"}, Usage: "label filter key=value (repeatable, AND)"}
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func printLabels(header string, labels map[string]string, indent int) {
	if len(labels) == 0 {
		fmt.Printf("%-*s%s\n", indent, header, "none")
		return
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
	sort.Strings(rest)
	parts = append(parts, rest...)
	s := strings.Join(parts, ", ")
	if len(s) > 48 {
		return s[:45] + "..."
	}
	return s
}

func formatExports(refs []v1.ExportDetailResponse) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Client
		if len(r.Labels) > 0 {
			s += " (" + formatLabelsShort(r.Labels) + ")"
		}
		if !r.CreatedAt.IsZero() {
			s += " since " + r.CreatedAt.Format(timeFmt)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func splitLabelsFlag(cmd *cli.Command) []string {
	raw := cmd.StringSlice("label")
	var out []string
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func parseLabelsFlag(cmd *cli.Command) map[string]string {
	raw := splitLabelsFlag(cmd)
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > config.MaxUserLabels {
		_, _ = fmt.Fprintf(os.Stderr, "warning: too many labels (%d), max %d user labels allowed\n", len(raw), config.MaxUserLabels)
		raw = raw[:config.MaxUserLabels]
	}
	labels := make(map[string]string, len(raw))
	for _, pair := range raw {
		k, v, _ := strings.Cut(pair, "=")
		if slices.Contains(config.SoftReservedLabelKeys, k) {
			_, _ = fmt.Fprintf(os.Stderr, "warning: label %q is reserved, skipping\n", k)
			continue
		}
		labels[k] = v
	}
	return labels
}

func sortFlag() cli.Flag {
	return &cli.StringFlag{Name: "sort", Aliases: []string{"s"}, Usage: "sort by: name, size, used, used%, created, clients"}
}

func ascFlag() cli.Flag {
	return &cli.BoolFlag{Name: "asc", Usage: "ascending sort (default is descending)"}
}

func watchFlag() cli.Flag {
	return &cli.DurationFlag{Name: "watch", Aliases: []string{"w"}, Value: 2 * time.Second, Usage: "watch mode with interval (default 2s)"}
}

func injectWatchDefault(args []string) []string {
	for i, a := range args {
		if a == "-w" || a == "--watch" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				out := make([]string, 0, len(args)+1)
				out = append(out, args[:i+1]...)
				out = append(out, "2s")
				out = append(out, args[i+1:]...)
				return out
			}
		}
	}
	return args
}

func runWatch(ctx context.Context, cmd *cli.Command, fn func() error) error {
	if !cmd.IsSet("watch") || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn()
	}
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
		fmt.Fprintf(os.Stderr, "\ntook %s, updated %s, refresh %s\n", fmtDuration(time.Since(start)), time.Now().Format("15:04:05"), interval)
		fmt.Print("\033[J")
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func columnsFlag() cli.Flag {
	return &cli.StringFlag{Name: "columns", Aliases: []string{"c"}, Usage: "comma-separated columns to display (omits header if single column)"}
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
			avail[strings.ToLower(col)] = col
		}
		var filtered []string
		for _, r := range strings.Split(raw, ",") {
			if col, ok := avail[strings.ToLower(strings.TrimSpace(r))]; ok {
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

func sortVolumes(vols []v1.VolumeResponse, field string, reverse bool) {
	sort.Slice(vols, func(i, j int) bool {
		var less bool
		switch field {
		case sortSize:
			less = vols[i].SizeBytes < vols[j].SizeBytes
		case sortUsed:
			less = vols[i].UsedBytes < vols[j].UsedBytes
		case sortUsedPct:
			less = usedPct(vols[i].UsedBytes, vols[i].SizeBytes) < usedPct(vols[j].UsedBytes, vols[j].SizeBytes)
		case sortCreated:
			less = vols[i].CreatedAt.Before(vols[j].CreatedAt)
		case sortExports:
			less = vols[i].Exports < vols[j].Exports
		default:
			less = vols[i].Name < vols[j].Name
		}
		if reverse {
			return !less
		}
		return less
	})
}

func sortVolumesDetail(vols []v1.VolumeDetailResponse, field string, reverse bool) {
	sort.Slice(vols, func(i, j int) bool {
		var less bool
		switch field {
		case sortSize:
			less = vols[i].SizeBytes < vols[j].SizeBytes
		case sortUsed:
			less = vols[i].UsedBytes < vols[j].UsedBytes
		case sortUsedPct:
			less = usedPct(vols[i].UsedBytes, vols[i].SizeBytes) < usedPct(vols[j].UsedBytes, vols[j].SizeBytes)
		case sortCreated:
			less = vols[i].CreatedAt.Before(vols[j].CreatedAt)
		case sortExports:
			less = len(vols[i].Exports) < len(vols[j].Exports)
		default:
			less = vols[i].Name < vols[j].Name
		}
		if reverse {
			return !less
		}
		return less
	})
}

func sortSnapshots(snaps []v1.SnapshotResponse, field string, reverse bool) {
	sort.Slice(snaps, func(i, j int) bool {
		var less bool
		switch field {
		case sortSize:
			less = snaps[i].SizeBytes < snaps[j].SizeBytes
		case sortUsed:
			less = snaps[i].UsedBytes < snaps[j].UsedBytes
		case sortCreated:
			less = snaps[i].CreatedAt.Before(snaps[j].CreatedAt)
		case sortVolume:
			less = snaps[i].Volume < snaps[j].Volume
		default:
			less = snaps[i].Name < snaps[j].Name
		}
		if reverse {
			return !less
		}
		return less
	})
}

func sortSnapshotsDetail(snaps []v1.SnapshotDetailResponse, field string, reverse bool) {
	sort.Slice(snaps, func(i, j int) bool {
		var less bool
		switch field {
		case sortSize:
			less = snaps[i].SizeBytes < snaps[j].SizeBytes
		case sortUsed:
			less = snaps[i].UsedBytes < snaps[j].UsedBytes
		case sortCreated:
			less = snaps[i].CreatedAt.Before(snaps[j].CreatedAt)
		case sortVolume:
			less = snaps[i].Volume < snaps[j].Volume
		default:
			less = snaps[i].Name < snaps[j].Name
		}
		if reverse {
			return !less
		}
		return less
	})
}

func sortExportsList(exports []v1.ExportResponse, field string, reverse bool) {
	sort.Slice(exports, func(i, j int) bool {
		var less bool
		switch field {
		case "client":
			less = exports[i].Client < exports[j].Client
		case sortCreated:
			less = exports[i].CreatedAt.Before(exports[j].CreatedAt)
		default:
			less = exports[i].Name < exports[j].Name
		}
		if reverse {
			return !less
		}
		return less
	})
}

func sortExportsDetailList(exports []v1.ExportDetailResponse, field string, reverse bool) {
	sort.Slice(exports, func(i, j int) bool {
		var less bool
		switch field {
		case "client":
			less = exports[i].Client < exports[j].Client
		case sortCreated:
			less = exports[i].CreatedAt.Before(exports[j].CreatedAt)
		default:
			less = exports[i].Name < exports[j].Name
		}
		if reverse {
			return !less
		}
		return less
	})
}

var (
	apiClient *v1.Client
	cmdStart  time.Time
)

func initClient(cmd *cli.Command) {
	apiClient = v1.NewClient(cmd.Root().String("agent-url"), cmd.Root().String("agent-token"), config.IdentityCLI)
}

func printTiming() {
	fmt.Fprintf(os.Stderr, "took %s, %s\n", fmtDuration(time.Since(cmdStart)), time.Now().Format("2006-01-02 15:04:05"))
}

func outputFormat(cmd *cli.Command) string {
	return cmd.Root().String("output")
}

func isWide(cmd *cli.Command) bool {
	o := outputFormat(cmd)
	return o == outputWide || strings.Contains(o, "wide")
}

func isJSON(cmd *cli.Command) bool {
	return strings.Contains(outputFormat(cmd), outputJSON)
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

func fmtDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func wrapErr(err error, resource, name string) error {
	if err == nil {
		return nil
	}
	switch {
	case v1.IsNotFound(err):
		return fmt.Errorf("%s %q not found", resource, name)
	case v1.IsConflict(err):
		return fmt.Errorf("%s %q already exists", resource, name)
	case v1.IsLocked(err):
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
