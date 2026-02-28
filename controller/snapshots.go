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

func (s *Server) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	agents := s.agents.Agents()

	type agentQuery struct {
		sc     string
		client *agentAPI.Client
		volume string
	}
	var queries []agentQuery

	if req.SnapshotId != "" {
		sc, _, err := parseVolumeID(req.SnapshotId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid snapshot ID: %v", err)
		}
		if c, ok := agents[sc]; ok {
			queries = append(queries, agentQuery{sc: sc, client: c})
		}
	} else if req.SourceVolumeId != "" {
		sc, volName, err := parseVolumeID(req.SourceVolumeId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid source volume ID: %v", err)
		}
		if c, ok := agents[sc]; ok {
			queries = append(queries, agentQuery{sc: sc, client: c, volume: volName})
		}
	} else {
		for sc, client := range agents {
			queries = append(queries, agentQuery{sc: sc, client: client})
		}
	}

	var entries []*csi.ListSnapshotsResponse_Entry
	for _, q := range queries {
		start := time.Now()
		var snapList *agentAPI.SnapshotListResponse
		var err error
		if q.volume != "" {
			snapList, err = q.client.ListVolumeSnapshots(ctx, q.volume)
		} else {
			snapList, err = q.client.ListSnapshots(ctx)
		}
		agentDuration.WithLabelValues("list_snapshots", q.sc).Observe(time.Since(start).Seconds())
		if err != nil {
			agentOpsTotal.WithLabelValues("list_snapshots", "error", q.sc).Inc()
			log.Warn().Err(err).Str("sc", q.sc).Msg("failed to list snapshots from agent")
			continue
		}
		agentOpsTotal.WithLabelValues("list_snapshots", "success", q.sc).Inc()

		for _, snap := range snapList.Snapshots {
			snapID := makeVolumeID(q.sc, snap.Name)

			if req.SnapshotId != "" && snapID != req.SnapshotId {
				continue
			}

			entries = append(entries, &csi.ListSnapshotsResponse_Entry{
				Snapshot: &csi.Snapshot{
					SnapshotId:     snapID,
					SourceVolumeId: makeVolumeID(q.sc, snap.Volume),
					SizeBytes:      int64(snap.SizeBytes),
					ReadyToUse:     true,
					CreationTime:   timestamppb.New(snap.CreatedAt),
				},
			})
		}
	}

	paged, nextToken, err := paginate(entries, req.StartingToken, req.MaxEntries)
	if err != nil {
		return nil, err
	}

	return &csi.ListSnapshotsResponse{
		Entries:   paged,
		NextToken: nextToken,
	}, nil
}

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
