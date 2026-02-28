package driver

import (
	"context"
	"encoding/json"
	"os"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

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

	// Try reading agent-written metadata from the staging path
	if req.StagingTargetPath != "" {
		metaPath := req.StagingTargetPath + "/" + config.MetadataFile
		if data, err := os.ReadFile(metaPath); err == nil {
			var vs volumeStats
			if err := json.Unmarshal(data, &vs); err == nil && vs.QuotaBytes > 0 {
				used := int64(vs.UsedBytes)
				total := int64(vs.QuotaBytes)
				avail := total - used
				if avail < 0 {
					avail = 0
				}
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
	return nil, status.Errorf(codes.Unavailable, "%s not available, agent may be down", config.MetadataFile)
}
