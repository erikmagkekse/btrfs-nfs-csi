package controller

import (
	"context"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot name required")
	}
	if req.SourceVolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "source volume ID required")
	}

	sc, volName, err := parseVolumeID(req.SourceVolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	snapResp, err := client.CreateSnapshot(ctx, agentAPI.SnapshotCreateRequest{
		Volume: volName,
		Name:   req.Name,
	})
	agentDuration.WithLabelValues("create_snapshot", sc).Observe(time.Since(start).Seconds())
	if err != nil {
		if agentAPI.IsConflict(err) {
			agentOpsTotal.WithLabelValues("create_snapshot", "conflict", sc).Inc()
			return &csi.CreateSnapshotResponse{
				Snapshot: &csi.Snapshot{
					SnapshotId:     makeVolumeID(sc, req.Name),
					SourceVolumeId: req.SourceVolumeId,
					ReadyToUse:     true,
					CreationTime:   timestamppb.Now(),
				},
			}, nil
		}
		agentOpsTotal.WithLabelValues("create_snapshot", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "create snapshot: %v", err)
	}
	agentOpsTotal.WithLabelValues("create_snapshot", "success", sc).Inc()

	log.Info().Str("snapshot", req.Name).Str("volume", volName).Msg("snapshot created")

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     makeVolumeID(sc, req.Name),
			SourceVolumeId: req.SourceVolumeId,
			SizeBytes:      int64(snapResp.SizeBytes),
			ReadyToUse:     true,
			CreationTime:   timestamppb.New(snapResp.CreatedAt),
		},
	}, nil
}

func (s *Server) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	if req.SnapshotId == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot ID required")
	}

	sc, name, err := parseVolumeID(req.SnapshotId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	deleteErr := client.DeleteSnapshot(ctx, name)
	agentDuration.WithLabelValues("delete_snapshot", sc).Observe(time.Since(start).Seconds())
	if deleteErr != nil {
		if agentAPI.IsNotFound(deleteErr) {
			agentOpsTotal.WithLabelValues("delete_snapshot", "not_found", sc).Inc()
			return &csi.DeleteSnapshotResponse{}, nil
		}
		agentOpsTotal.WithLabelValues("delete_snapshot", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "delete snapshot: %v", deleteErr)
	}
	agentOpsTotal.WithLabelValues("delete_snapshot", "success", sc).Inc()

	log.Info().Str("snapshot", name).Msg("snapshot deleted")

	return &csi.DeleteSnapshotResponse{}, nil
}
