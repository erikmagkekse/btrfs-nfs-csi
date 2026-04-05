package driver

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"syscall"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/csiserver"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
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
		stagingPath = s.findStagingPath(req.VolumeId)
	}

	sc, vol := parseVolumeLog(req.VolumeId)

	log.Debug().Str("volume", vol).Str("sc", sc).Str("volumePath", req.VolumePath).Str("stagingPath", stagingPath).Msg("looking up volume stats")

	if stagingPath != "" {
		metaPath := stagingPath + "/" + config.MetadataFile
		data, err := os.ReadFile(metaPath)
		if err != nil {
			log.Warn().Err(err).Str("volume", vol).Str("sc", sc).Str("path", metaPath).Msg("failed to read metadata")
		} else {
			var vs volumeStats
			if err := json.Unmarshal(data, &vs); err != nil {
				log.Warn().Err(err).Str("volume", vol).Str("sc", sc).Str("path", metaPath).Msg("metadata JSON corrupt")
			} else {
				if vs.QuotaBytes > 0 {
					used := int64(vs.UsedBytes)
					total := int64(vs.QuotaBytes)
					avail := total - used
					if avail < 0 {
						avail = 0
					}
					volumeStatsOpsTotal.WithLabelValues("success").Inc()
					resp := &csi.NodeGetVolumeStatsResponse{
						Usage: []*csi.VolumeUsage{{
							Available: avail,
							Total:     total,
							Used:      used,
							Unit:      csi.VolumeUsage_BYTES,
						}},
					}
					s.attachVolumeCondition(req.VolumeId, resp)
					return resp, nil
				}
				// Quota disabled: fallback to statfs
				log.Debug().Str("volume", vol).Str("sc", sc).Msg("quota not configured, falling back to statfs")
				resp, err := statfsResponse(req.VolumePath)
				if err == nil {
					s.attachVolumeCondition(req.VolumeId, resp)
				}
				return resp, err
			}
		}
	}

	volumeStatsOpsTotal.WithLabelValues("error").Inc()
	if stagingPath == "" {
		return nil, status.Errorf(codes.NotFound, "staging path not found for volume %s", req.VolumeId)
	}
	return nil, status.Errorf(codes.Internal, "failed to read %s for volume %s from %s", config.MetadataFile, req.VolumeId, stagingPath)
}

// findStagingPath finds the globalmount staging path for a volume by scanning active mounts.
func (s *NodeServer) findStagingPath(volumeId string) string {
	_, volName, err := csiserver.ParseVolumeID(volumeId)
	if err != nil {
		log.Debug().Err(err).Str("volume", volumeId).Msg("failed to parse volume ID for staging path lookup")
		return ""
	}

	mounts, err := s.mounter.List()
	if err != nil {
		log.Warn().Err(err).Msg("failed to list mounts")
		return ""
	}

	for _, mp := range mounts {
		if (mp.Type != "nfs" && mp.Type != "nfs4") || !strings.Contains(mp.Path, "globalmount") {
			continue
		}
		if strings.HasSuffix(mp.Device, "/"+volName) {
			return mp.Path
		}
	}

	log.Debug().Str("volume", volumeId).Msg("no staging path found in mounts")
	return ""
}

func (s *NodeServer) attachVolumeCondition(volumeID string, resp *csi.NodeGetVolumeStatsResponse) {
	if h, ok := s.healthState.Load(volumeID); ok {
		vh := h.(*volumeHealth)
		if vh.abnormal {
			resp.VolumeCondition = &csi.VolumeCondition{
				Abnormal: true,
				Message:  vh.message,
			}
		}
	}
}

func statfsResponse(path string) (*csi.NodeGetVolumeStatsResponse, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		volumeStatsOpsTotal.WithLabelValues("error").Inc()
		return nil, status.Errorf(codes.Internal, "statfs failed on %s: %v", path, err)
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	free := int64(st.Bavail) * int64(st.Bsize)
	volumeStatsOpsTotal.WithLabelValues("success").Inc()
	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{{
			Available: free,
			Total:     total,
			Used:      total - free,
			Unit:      csi.VolumeUsage_BYTES,
		}},
	}, nil
}
