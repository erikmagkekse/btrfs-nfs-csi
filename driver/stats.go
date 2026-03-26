package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type volumeStats struct {
	QuotaBytes uint64 `json:"quota_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

// NodeGetVolumeStats returns quota-aware usage from metadata.json written by the agent's UsageUpdater.
// Freshness depends on AGENT_FEATURE_QUOTA_UPDATE_INTERVAL (default 1m), which matches kubelet's polling interval.
func (s *NodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if req.VolumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume path required")
	}

	// Kubelet does not populate StagingTargetPath in NodeGetVolumeStats (kubernetes/kubernetes#109585).
	// Resolve the staging path from /proc/self/mountinfo by matching the volume name in NFS sources.
	stagingPath := req.StagingTargetPath
	if stagingPath == "" {
		stagingPath = findStagingPath(req.VolumeId)
	}

	if stagingPath != "" {
		metaPath := stagingPath + "/" + config.MetadataFile
		if data, err := os.ReadFile(metaPath); err == nil {
			var vs volumeStats
			if err := json.Unmarshal(data, &vs); err == nil && vs.QuotaBytes > 0 {
				used := int64(vs.UsedBytes)
				total := int64(vs.QuotaBytes)
				avail := total - used
				if avail < 0 {
					avail = 0
				}
				volumeStatsOpsTotal.WithLabelValues("success").Inc()
				return &csi.NodeGetVolumeStatsResponse{
					Usage: []*csi.VolumeUsage{{
						Available: avail,
						Total:     total,
						Used:      used,
						Unit:      csi.VolumeUsage_BYTES,
					}},
				}, nil
			}
		}
	}

	// No statfs fallback - returns NFS-level data which doesn't reflect per-volume quota.
	// Better to return an error so kubelet retries than to report misleading capacity.
	volumeStatsOpsTotal.WithLabelValues("error").Inc()
	return nil, status.Errorf(codes.Unavailable, "%s not available, agent may be down", config.MetadataFile)
}

// findStagingPath parses /proc/self/mountinfo to find the globalmount staging path for a volume.
// It extracts the volume name from the volumeId (format "storageClass|pvcName") and matches it
// against NFS mount sources whose mountpoint contains "globalmount".
func findStagingPath(volumeId string) string {
	_, volName, err := utils.ParseVolumeID(volumeId)
	if err != nil {
		return ""
	}

	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), " ")
		if len(fields) < 10 {
			continue
		}
		mountpoint := fields[4]
		if !strings.Contains(mountpoint, "globalmount") {
			continue
		}
		sepIdx := len(fields) - 4
		for sepIdx > 5 && fields[sepIdx] != "-" {
			sepIdx--
		}
		if fields[sepIdx] != "-" {
			continue
		}
		fstype := fields[sepIdx+1]
		if fstype != "nfs" && fstype != "nfs4" {
			continue
		}
		source := fields[sepIdx+2]
		if strings.HasSuffix(source, "/"+volName) {
			return mountpoint
		}
	}
	return ""
}
