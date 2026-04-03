package ctl

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/urfave/cli/v3"
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
	sortClients = "clients"
	sortVolume  = "volume"
)

func sortFlag() cli.Flag {
	return &cli.StringFlag{Name: "sort", Aliases: []string{"s"}, Usage: "sort by: name, size, used, used%, created, clients"}
}

func ascFlag() cli.Flag {
	return &cli.BoolFlag{Name: "asc", Usage: "ascending sort (default is descending)"}
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
		case sortClients:
			less = vols[i].Clients < vols[j].Clients
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
		case sortClients:
			less = len(vols[i].Clients) < len(vols[j].Clients)
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

func clientFrom(cmd *cli.Command) *v1.Client {
	return v1.NewClient(cmd.Root().String("agent-url"), cmd.Root().String("agent-token"))
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
