package driver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and staging target path required")
	}

	unlock := s.volumeLock(req.VolumeId)
	defer unlock()

	vc := req.VolumeContext
	nfsServer := vc[config.ParamNFSServer]
	nfsSharePath := vc[config.ParamNFSSharePath]
	if nfsServer == "" || nfsSharePath == "" {
		return nil, status.Error(codes.InvalidArgument, "missing nfsServer or nfsSharePath in volume context")
	}

	stagingPath := req.StagingTargetPath

	if notMnt, _ := s.mounter.IsLikelyNotMountPoint(stagingPath); !notMnt {
		log.Debug().Str("path", stagingPath).Msg("already mounted at staging path")
		return &csi.NodeStageVolumeResponse{}, nil
	}

	if err := os.MkdirAll(stagingPath, 0755); err != nil {
		return nil, status.Errorf(codes.Internal, "mkdir staging: %v", err)
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
	if extra := vc[config.ParamNFSMountOptions]; extra != "" {
		opts = append(opts, strings.Split(extra, ",")...)
	}

	log.Info().Str("source", source).Str("target", stagingPath).Msg("mounting NFS")

	start := time.Now()
	err := s.mounter.Mount(source, stagingPath, "nfs", opts)
	mountDuration.WithLabelValues("nfs_mount").Observe(time.Since(start).Seconds())
	if err != nil {
		mountOpsTotal.WithLabelValues("nfs_mount", "error").Inc()
		return nil, status.Errorf(codes.Internal, "mount NFS: %v", err)
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

	if err := cleanupMountPoint(ctx, s.mounter, req.StagingTargetPath); err != nil {
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

	if notMnt, _ := s.mounter.IsLikelyNotMountPoint(req.TargetPath); !notMnt {
		log.Info().Str("path", req.TargetPath).Msg("already mounted, skipping publish")
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if err := os.MkdirAll(req.TargetPath, 0755); err != nil {
		return nil, status.Errorf(codes.Internal, "mkdir target: %v", err)
	}

	dataDir := req.StagingTargetPath + "/" + config.DataDir
	start := time.Now()
	err := s.mounter.Mount(dataDir, req.TargetPath, "", []string{"bind"})
	mountDuration.WithLabelValues("bind_mount").Observe(time.Since(start).Seconds())
	if err != nil {
		mountOpsTotal.WithLabelValues("bind_mount", "error").Inc()
		return nil, status.Errorf(codes.Internal, "bind mount: %v", err)
	}
	mountOpsTotal.WithLabelValues("bind_mount", "success").Inc()

	if req.Readonly {
		start = time.Now()
		err = s.mounter.Mount("", req.TargetPath, "", []string{"bind", "remount", "ro"})
		mountDuration.WithLabelValues("remount_ro").Observe(time.Since(start).Seconds())
		if err != nil {
			mountOpsTotal.WithLabelValues("remount_ro", "error").Inc()
			_ = cleanupMountPoint(ctx, s.mounter, req.TargetPath)
			return nil, status.Errorf(codes.Internal, "remount ro: %v", err)
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

	if err := cleanupMountPoint(ctx, s.mounter, req.TargetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "cleanup target: %v", err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}
