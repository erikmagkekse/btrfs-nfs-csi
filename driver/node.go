package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/model"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Mount operations inspired by kubernetes-csi/csi-driver-nfs:
// - Volume locks to prevent concurrent mount/unmount races
// - Mount timeout (2min) for stuck NFS mounts
// - Force unmount fallback for stuck mounts
// - Device-based mount point detection (like mount-utils IsLikelyNotMountPoint)
// See: https://github.com/kubernetes-csi/csi-driver-nfs

type NodeServer struct {
	csi.UnimplementedNodeServer
	nodeID string
	nodeIP string
	locks  sync.Map
}

func (s *NodeServer) volumeLock(id string) func() {
	val, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and staging target path required")
	}

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	vc := req.VolumeContext
	nfsServer := vc[model.ParamNFSServer]
	nfsSharePath := vc[model.ParamNFSSharePath]
	if nfsServer == "" || nfsSharePath == "" {
		return nil, status.Error(codes.InvalidArgument, "missing nfsServer or nfsSharePath in volume context")
	}

	stagingPath := req.StagingTargetPath

	if isMountPoint(stagingPath) {
		log.Debug().Str("path", stagingPath).Msg("already mounted at staging path")
		return &csi.NodeStageVolumeResponse{}, nil
	}

	if err := os.MkdirAll(stagingPath, 0755); err != nil {
		return nil, status.Errorf(codes.Internal, "mkdir staging: %v", err)
	}

	source := fmt.Sprintf("%s:%s", nfsServer, nfsSharePath)

	args := []string{"-t", "nfs"}
	mountOpts := "rw"
	if opts := vc[model.ParamNFSMountOptions]; opts != "" {
		mountOpts = mountOpts + "," + opts
	}
	args = append(args, "-o", mountOpts)
	args = append(args, source, stagingPath)

	log.Info().Str("source", source).Str("target", stagingPath).Msg("mounting NFS")

	mountCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	start := time.Now()
	out, err := exec.CommandContext(mountCtx, "mount", args...).CombinedOutput()
	mountDuration.WithLabelValues("nfs_mount").Observe(time.Since(start).Seconds())
	if err != nil {
		mountOpsTotal.WithLabelValues("nfs_mount", "error").Inc()
		return nil, status.Errorf(codes.Internal, "mount NFS: %v: %s", err, string(out))
	}
	mountOpsTotal.WithLabelValues("nfs_mount", "success").Inc()

	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and staging target path required")
	}

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	if err := cleanupMountPoint(ctx, req.StagingTargetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "cleanup staging: %v", err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" || req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID, staging target path, and target path required")
	}

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	if isMountPoint(req.TargetPath) {
		log.Info().Str("path", req.TargetPath).Msg("already mounted, skipping publish")
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if err := os.MkdirAll(req.TargetPath, 0755); err != nil {
		return nil, status.Errorf(codes.Internal, "mkdir target: %v", err)
	}

	mountCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	dataDir := req.StagingTargetPath + "/data"
	start := time.Now()
	out, err := exec.CommandContext(mountCtx, "mount", "--bind", dataDir, req.TargetPath).CombinedOutput()
	mountDuration.WithLabelValues("bind_mount").Observe(time.Since(start).Seconds())
	if err != nil {
		mountOpsTotal.WithLabelValues("bind_mount", "error").Inc()
		return nil, status.Errorf(codes.Internal, "bind mount: %v: %s", err, string(out))
	}
	mountOpsTotal.WithLabelValues("bind_mount", "success").Inc()

	if mount := req.VolumeCapability.GetMount(); mount != nil && mount.VolumeMountGroup != "" {
		gid, err := strconv.Atoi(mount.VolumeMountGroup)
		if err == nil {
			chownErr := os.Chown(req.TargetPath, -1, gid)
			chmodErr := os.Chmod(req.TargetPath, os.FileMode(0o2770))
			if chownErr != nil || chmodErr != nil {
				mountOpsTotal.WithLabelValues("fsgroup-chown", "error").Inc()
				log.Error().AnErr("chown", chownErr).AnErr("chmod", chmodErr).Str("path", req.TargetPath).Int("gid", gid).Msg("fsGroup failed")
			} else {
				mountOpsTotal.WithLabelValues("fsgroup-chown", "success").Inc()
				log.Info().Int("gid", gid).Str("path", req.TargetPath).Msg("fsGroup applied")
			}
		}
	} else {
		log.Warn().Msg("no volumeMountGroup in request - pod securityContext.fsGroup set?")
	}

	if req.Readonly {
		start = time.Now()
		out, err = exec.CommandContext(mountCtx, "mount", "-o", "remount,ro,bind", req.TargetPath).CombinedOutput()
		mountDuration.WithLabelValues("remount_ro").Observe(time.Since(start).Seconds())
		if err != nil {
			mountOpsTotal.WithLabelValues("remount_ro", "error").Inc()
			_ = forceUnmount(ctx, req.TargetPath)
			return nil, status.Errorf(codes.Internal, "remount ro: %v: %s", err, string(out))
		}
		mountOpsTotal.WithLabelValues("remount_ro", "success").Inc()
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.VolumeId == "" || req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and target path required")
	}

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	if err := cleanupMountPoint(ctx, req.TargetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "cleanup target: %v", err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *NodeServer) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_VOLUME_MOUNT_GROUP,
					},
				},
			},
		},
	}, nil
}

func (s *NodeServer) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: s.nodeID + "|" + s.nodeIP,
	}, nil
}

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
		metaPath := req.StagingTargetPath + "/metadata.json"
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
	return nil, status.Error(codes.Unavailable, "metadata.json not available, agent may be down")
}
