package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/urfave/cli/v3"
)

func labelFlag() cli.Flag {
	return &cli.StringSliceFlag{Name: "label", Aliases: []string{"l"}, Usage: "label filter key=value (repeatable, AND)"}
}

func allFlag() cli.Flag {
	return &cli.BoolFlag{Name: "all", Aliases: []string{"A"}, Usage: "show all (default: only created-by=cli)"}
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

func columnsFlag() cli.Flag {
	return &cli.StringFlag{Name: "columns", Aliases: []string{"c"}, Usage: "comma-separated columns to display (omits header if single column)"}
}

func splitLabelsFlag(cmd *cli.Command) []string {
	raw := cmd.StringSlice("label")
	var out []string
	for _, entry := range raw {
		for part := range strings.SplitSeq(entry, ",") {
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

type cliListOpts struct {
	models.ListOpts
	allSet   bool
	labelSet bool
}

func buildListOpts(cmd *cli.Command) cliListOpts {
	labels := splitLabelsFlag(cmd)
	allSet := cmd.Bool("all")
	if !allSet {
		labels = append(labels, config.LabelCreatedBy+"="+config.IdentityCLI)
	}
	return cliListOpts{
		ListOpts: models.ListOpts{Labels: labels},
		allSet:   allSet,
		labelSet: cmd.IsSet("label"),
	}
}

// injectWatchDefault inserts "2s" after bare -w/--watch flags so urfave/cli
// can parse them as DurationFlag (which always requires a value).
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

func emptyHint(resource string, count int, allSet, labelSet bool) {
	if count > 0 {
		return
	}
	msg := "no " + resource + " found"
	if labelSet {
		msg += " (label filter active)"
	} else if !allSet {
		msg += " (use -A to show all)"
	}
	fmt.Printf("  %s\n", msg)
}
