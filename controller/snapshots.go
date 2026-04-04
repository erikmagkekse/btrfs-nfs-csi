package controller

import (
	"context"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/csiserver"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	agents := s.agents.Agents()

	// single-agent queries (by snapshot or source volume ID)
	if req.SnapshotId != "" {
		sc, _, err := csiserver.ParseVolumeID(req.SnapshotId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid snapshot ID: %v", err)
		}
		c, ok := agents[sc]
		if !ok {
			return &csi.ListSnapshotsResponse{}, nil
		}
		snapList, err := c.ListSnapshots(ctx, agentAPI.ListOpts{})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list snapshots from %s: %v", sc, err)
		}
		var entries []*csi.ListSnapshotsResponse_Entry
		for _, snap := range snapList.Snapshots {
			snapID := csiserver.MakeVolumeID(sc, snap.Name)
			if snapID != req.SnapshotId {
				continue
			}
			entries = append(entries, &csi.ListSnapshotsResponse_Entry{
				Snapshot: &csi.Snapshot{
					SnapshotId:     snapID,
					SourceVolumeId: csiserver.MakeVolumeID(sc, snap.Volume),
					SizeBytes:      int64(snap.SizeBytes),
					ReadyToUse:     true,
					CreationTime:   timestamppb.New(snap.CreatedAt),
				},
			})
		}
		return &csi.ListSnapshotsResponse{Entries: entries}, nil
	}

	if req.SourceVolumeId != "" {
		sc, volName, err := csiserver.ParseVolumeID(req.SourceVolumeId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid source volume ID: %v", err)
		}
		c, ok := agents[sc]
		if !ok {
			return &csi.ListSnapshotsResponse{}, nil
		}
		snapList, err := c.ListVolumeSnapshots(ctx, volName, agentAPI.ListOpts{})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list snapshots from %s: %v", sc, err)
		}
		var entries []*csi.ListSnapshotsResponse_Entry
		for _, snap := range snapList.Snapshots {
			entries = append(entries, &csi.ListSnapshotsResponse_Entry{
				Snapshot: &csi.Snapshot{
					SnapshotId:     csiserver.MakeVolumeID(sc, snap.Name),
					SourceVolumeId: csiserver.MakeVolumeID(sc, snap.Volume),
					SizeBytes:      int64(snap.SizeBytes),
					ReadyToUse:     true,
					CreationTime:   timestamppb.New(snap.CreatedAt),
				},
			})
		}
		return &csi.ListSnapshotsResponse{Entries: entries}, nil
	}

	// multi-agent paginated query
	pt, err := decodePageToken(req.StartingToken)
	if err != nil {
		return nil, err
	}

	sorted := sortedAgents(agents)
	limit := int(req.MaxEntries)

	var entries []*csi.ListSnapshotsResponse_Entry
	var nextToken string

	for _, a := range sorted {
		if pt.SC != "" && a.sc < pt.SC {
			continue
		}
		after := ""
		if a.sc == pt.SC {
			after = pt.After
		}

		start := time.Now()
		snapList, err := a.client.ListSnapshots(ctx, agentAPI.ListOpts{After: after, Limit: limit})
		agentDuration.WithLabelValues("list_snapshots", a.sc).Observe(time.Since(start).Seconds())
		if err != nil {
			agentOpsTotal.WithLabelValues("list_snapshots", "error", a.sc).Inc()
			log.Warn().Err(err).Str("sc", a.sc).Msg("failed to list snapshots from agent")
			continue
		}
		agentOpsTotal.WithLabelValues("list_snapshots", "success", a.sc).Inc()

		for _, snap := range snapList.Snapshots {
			entries = append(entries, &csi.ListSnapshotsResponse_Entry{
				Snapshot: &csi.Snapshot{
					SnapshotId:     csiserver.MakeVolumeID(a.sc, snap.Name),
					SourceVolumeId: csiserver.MakeVolumeID(a.sc, snap.Volume),
					SizeBytes:      int64(snap.SizeBytes),
					ReadyToUse:     true,
					CreationTime:   timestamppb.New(snap.CreatedAt),
				},
			})
		}

		if snapList.Next != "" {
			nextToken = encodePageToken(a.sc, snapList.Next)
			break
		}

		if limit > 0 {
			limit -= len(snapList.Snapshots)
			if limit <= 0 {
				break
			}
		}
	}

	return &csi.ListSnapshotsResponse{
		Entries:   entries,
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

	sc, volName, err := csiserver.ParseVolumeID(req.SourceVolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("snapshot", req.Name).Str("volume", volName).Str("sc", sc).Msg("failed to create agent client for snapshot")
		return nil, err
	}

	log.Debug().Str("snapshot", req.Name).Str("volume", volName).Str("sc", sc).Msg("creating snapshot")

	start := time.Now()
	labels := s.resolveSnapshotLabels(ctx, req.Parameters, req.SourceVolumeId, sc, volName)
	snapResp, err := client.CreateSnapshot(ctx, agentAPI.SnapshotCreateRequest{
		Volume: volName,
		Name:   req.Name,
		Labels: labels,
	})
	agentDuration.WithLabelValues("create_snapshot", sc).Observe(time.Since(start).Seconds())
	if err != nil {
		if agentAPI.IsConflict(err) {
			agentOpsTotal.WithLabelValues("create_snapshot", "conflict", sc).Inc()
			return &csi.CreateSnapshotResponse{
				Snapshot: &csi.Snapshot{
					SnapshotId:     csiserver.MakeVolumeID(sc, req.Name),
					SourceVolumeId: req.SourceVolumeId,
					ReadyToUse:     true,
					CreationTime:   timestamppb.Now(),
				},
			}, nil
		}
		agentOpsTotal.WithLabelValues("create_snapshot", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "create snapshot %s from volume %s via %s: %v", req.Name, volName, sc, err)
	}
	agentOpsTotal.WithLabelValues("create_snapshot", "success", sc).Inc()

	log.Info().Str("snapshot", req.Name).Str("volume", volName).Str("sc", sc).Msg("snapshot created")

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     csiserver.MakeVolumeID(sc, req.Name),
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

	sc, name, err := csiserver.ParseVolumeID(req.SnapshotId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("snapshot", name).Str("sc", sc).Msg("failed to create agent client for snapshot delete")
		return nil, err
	}

	log.Debug().Str("snapshot", name).Str("sc", sc).Msg("deleting snapshot")

	start := time.Now()
	deleteErr := client.DeleteSnapshot(ctx, name)
	agentDuration.WithLabelValues("delete_snapshot", sc).Observe(time.Since(start).Seconds())
	if deleteErr != nil {
		if agentAPI.IsNotFound(deleteErr) {
			agentOpsTotal.WithLabelValues("delete_snapshot", "not_found", sc).Inc()
			log.Info().Str("snapshot", name).Str("sc", sc).Msg("snapshot already deleted")
			return &csi.DeleteSnapshotResponse{}, nil
		}
		agentOpsTotal.WithLabelValues("delete_snapshot", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "delete snapshot %s via %s: %v", name, sc, deleteErr)
	}
	agentOpsTotal.WithLabelValues("delete_snapshot", "success", sc).Inc()

	log.Info().Str("snapshot", name).Str("sc", sc).Msg("snapshot deleted")

	return &csi.DeleteSnapshotResponse{}, nil
}
