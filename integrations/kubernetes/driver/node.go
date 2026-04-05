package driver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/csiserver"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/mount-utils"
)

func (s *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and staging target path required")
	}

	sc, vol := parseVolumeLog(req.VolumeId)

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	vc := req.VolumeContext
	nfsServer := vc[csiserver.ParamNFSServer]
	nfsSharePath := vc[csiserver.ParamNFSSharePath]
	if nfsServer == "" || nfsSharePath == "" {
		return nil, status.Error(codes.InvalidArgument, "missing nfsServer or nfsSharePath in volume context")
	}

	stagingPath := req.StagingTargetPath

	// Healthy mount check: stat the data dir to see if staging is already done.
	// Stale staging mounts are handled by the background health checker, not here.
	if err := statWithTimeout(stagingPath+"/"+config.DataDir, staleCheckTimeout); err == nil {
		log.Info().Str("volume", vol).Str("sc", sc).Str("path", stagingPath).Msg("already mounted at staging path")
		return &csi.NodeStageVolumeResponse{}, nil
	}

	if err := os.MkdirAll(stagingPath, 0755); err != nil {
		return nil, status.Errorf(codes.Internal, "mkdir staging for volume %s: %v", vol, err)
	}

	source := fmt.Sprintf("%s:%s", nfsServer, nfsSharePath)

	var opts []string
	opts = append(opts, "rw")
	if vc := req.GetVolumeCapability(); vc != nil {
		if am := vc.GetAccessMode(); am != nil &&
			(am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
				am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY) {
			opts = []string{"ro"}
		}
	}
	if extra := vc[csiserver.ParamNFSMountOptions]; extra != "" {
		opts = append(opts, strings.Split(extra, ",")...)
	}

	log.Debug().Str("volume", vol).Str("sc", sc).Str("source", source).Str("target", stagingPath).Strs("opts", opts).Msg("mounting NFS")

	start := time.Now()
	err := s.mounter.Mount(source, stagingPath, "nfs", opts)
	elapsed := time.Since(start)
	mountDuration.WithLabelValues("nfs_mount").Observe(elapsed.Seconds())
	if err != nil {
		mountOpsTotal.WithLabelValues("nfs_mount", "error").Inc()
		return nil, status.Errorf(codes.Internal, "mount NFS for volume %s: %v", vol, err)
	}
	mountOpsTotal.WithLabelValues("nfs_mount", "success").Inc()

	log.Info().Str("volume", vol).Str("sc", sc).Str("node", s.nodeID).Str("source", source).Str("target", stagingPath).Str("took", elapsed.String()).Msg("stage complete")
	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and staging target path required")
	}

	sc, vol := parseVolumeLog(req.VolumeId)

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	log.Debug().Str("volume", vol).Str("sc", sc).Str("node", s.nodeID).Str("path", req.StagingTargetPath).Msg("unstaging volume")

	if err := cleanupMountPoint(ctx, s.mounter, req.StagingTargetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "cleanup staging for volume %s at %s: %v", vol, req.StagingTargetPath, err)
	}

	s.healthState.Delete(req.VolumeId)

	log.Info().Str("volume", vol).Str("sc", sc).Str("node", s.nodeID).Str("path", req.StagingTargetPath).Msg("unstage complete")
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" || req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID, staging target path, and target path required")
	}

	sc, vol := parseVolumeLog(req.VolumeId)

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	if err := statWithTimeout(req.TargetPath, staleCheckTimeout); err == nil {
		log.Info().Str("volume", vol).Str("sc", sc).Str("path", req.TargetPath).Msg("already mounted, skipping publish")
		return &csi.NodePublishVolumeResponse{}, nil
	} else if mount.IsCorruptedMnt(err) || errors.Is(err, errStatTimeout) {
		log.Warn().Err(err).Str("volume", vol).Str("sc", sc).Str("path", req.TargetPath).Msg("stale bind mount detected, remounting over it")
	}

	if err := os.MkdirAll(req.TargetPath, 0755); err != nil {
		return nil, status.Errorf(codes.Internal, "mkdir target for volume %s: %v", vol, err)
	}

	dataDir := req.StagingTargetPath + "/" + config.DataDir
	log.Debug().Str("volume", vol).Str("sc", sc).Str("source", dataDir).Str("target", req.TargetPath).Bool("readonly", req.Readonly).Msg("bind mounting")

	start := time.Now()
	err := s.mounter.Mount(dataDir, req.TargetPath, "", []string{"bind"})
	elapsed := time.Since(start)
	mountDuration.WithLabelValues("bind_mount").Observe(elapsed.Seconds())
	if err != nil {
		mountOpsTotal.WithLabelValues("bind_mount", "error").Inc()
		return nil, status.Errorf(codes.Internal, "bind mount for volume %s: %v", vol, err)
	}
	mountOpsTotal.WithLabelValues("bind_mount", "success").Inc()

	if req.Readonly {
		start = time.Now()
		err = s.mounter.Mount("", req.TargetPath, "", []string{"bind", "remount", "ro"})
		mountDuration.WithLabelValues("remount_ro").Observe(time.Since(start).Seconds())
		if err != nil {
			mountOpsTotal.WithLabelValues("remount_ro", "error").Inc()
			if cleanErr := cleanupMountPoint(ctx, s.mounter, req.TargetPath); cleanErr != nil {
				log.Error().Err(cleanErr).Str("volume", vol).Str("sc", sc).Str("path", req.TargetPath).Msg("cleanup after remount-ro failure also failed")
			}
			return nil, status.Errorf(codes.Internal, "remount ro for volume %s: %v", vol, err)
		}
		mountOpsTotal.WithLabelValues("remount_ro", "success").Inc()
	}

	log.Info().Str("volume", vol).Str("sc", sc).Str("node", s.nodeID).Str("target", req.TargetPath).Bool("readonly", req.Readonly).Str("took", elapsed.String()).Msg("publish complete")
	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.VolumeId == "" || req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and target path required")
	}

	sc, vol := parseVolumeLog(req.VolumeId)

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	log.Debug().Str("volume", vol).Str("sc", sc).Str("node", s.nodeID).Str("path", req.TargetPath).Msg("unpublishing volume")

	if err := cleanupMountPoint(ctx, s.mounter, req.TargetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "cleanup target for volume %s at %s: %v", vol, req.TargetPath, err)
	}

	log.Info().Str("volume", vol).Str("sc", sc).Str("node", s.nodeID).Str("path", req.TargetPath).Msg("unpublish complete")
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// statWithTimeout runs os.Stat in a goroutine with a timeout.
// Returns nil if stat succeeds, error if it fails or times out.
// Needed because os.Stat on a stale NFS hard mount hangs indefinitely.
func statWithTimeout(path string, timeout time.Duration) error {
	ch := make(chan error, 1)
	go func() {
		_, err := os.Stat(path)
		ch <- err
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("stat %s after %s: %w", path, timeout, errStatTimeout)
	}
}
