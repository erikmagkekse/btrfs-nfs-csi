package controller

import (
	"context"
	"strconv"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/model"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	agents := s.agents.Agents()

	var entries []*csi.ListVolumesResponse_Entry
	for sc, client := range agents {
		start := time.Now()
		volList, err := client.ListVolumes(ctx)
		agentDuration.WithLabelValues("list_volumes", sc).Observe(time.Since(start).Seconds())
		if err != nil {
			agentOpsTotal.WithLabelValues("list_volumes", "error", sc).Inc()
			log.Warn().Err(err).Str("sc", sc).Msg("failed to list volumes from agent")
			continue
		}
		agentOpsTotal.WithLabelValues("list_volumes", "success", sc).Inc()

		for _, vol := range volList.Volumes {
			entries = append(entries, &csi.ListVolumesResponse_Entry{
				Volume: &csi.Volume{
					VolumeId:      makeVolumeID(sc, vol.Name),
					CapacityBytes: int64(vol.SizeBytes),
				},
			})
		}
	}

	paged, nextToken, err := paginate(entries, req.StartingToken, req.MaxEntries)
	if err != nil {
		return nil, err
	}

	return &csi.ListVolumesResponse{
		Entries:   paged,
		NextToken: nextToken,
	}, nil
}

func (s *Server) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name required")
	}

	params := req.Parameters
	nfsServer := params[model.ParamNFSServer]
	agentURL := params[paramAgentURL]
	if nfsServer == "" || agentURL == "" {
		return nil, status.Error(codes.InvalidArgument, "nfsServer and agentURL parameters required")
	}

	client, err := agentClientFromSecrets(agentURL, req.Secrets)
	if err != nil {
		return nil, err
	}

	s.agents.Track(agentURL, client)
	sc := s.agents.StorageClass(agentURL)

	var sizeBytes uint64 = 1 << 30 // 1 GiB default
	if req.CapacityRange != nil {
		if req.CapacityRange.RequiredBytes > 0 {
			sizeBytes = uint64(req.CapacityRange.RequiredBytes)
		} else if req.CapacityRange.LimitBytes > 0 {
			sizeBytes = uint64(req.CapacityRange.LimitBytes)
		}
	}

	volCtx := map[string]string{
		model.ParamNFSServer: nfsServer,
		paramAgentURL:        agentURL,
	}
	if opts := params[model.ParamNFSMountOptions]; opts != "" {
		volCtx[model.ParamNFSMountOptions] = opts
	}
	if n := params[model.PvcNameKey]; n != "" {
		volCtx[model.PvcNameKey] = n
	}
	if ns := params[model.PvcNamespaceKey]; ns != "" {
		volCtx[model.PvcNamespaceKey] = ns
	}

	vp := resolveVolumeParams(ctx, params)

	// Clone from snapshot
	if req.VolumeContentSource != nil {
		snap := req.VolumeContentSource.GetSnapshot()
		if snap == nil {
			return nil, status.Error(codes.InvalidArgument, "only snapshot content source is supported")
		}
		_, snapName, err := parseVolumeID(snap.SnapshotId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid snapshot ID: %v", err)
		}

		start := time.Now()
		cloneResp, err := client.CreateClone(ctx, agentAPI.CloneCreateRequest{
			Snapshot: snapName,
			Name:     req.Name,
		})
		agentDuration.WithLabelValues("create_clone", sc).Observe(time.Since(start).Seconds())
		if err != nil {
			if agentAPI.IsConflict(err) {
				agentOpsTotal.WithLabelValues("create_clone", "conflict", sc).Inc()
				if cloneResp == nil {
					return nil, status.Errorf(codes.Internal, "clone conflict but no metadata returned: %v", err)
				}
			} else {
				agentOpsTotal.WithLabelValues("create_clone", "error", sc).Inc()
				return nil, status.Errorf(codes.Internal, "create clone: %v", err)
			}
		} else {
			agentOpsTotal.WithLabelValues("create_clone", "success", sc).Inc()
		}
		volCtx[model.ParamNFSSharePath] = cloneResp.Path

		log.Info().Str("volume", req.Name).Str("snapshot", snapName).Msg("volume cloned from snapshot")

		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				VolumeId:      makeVolumeID(sc, req.Name),
				CapacityBytes: int64(sizeBytes),
				VolumeContext: volCtx,
				ContentSource: req.VolumeContentSource,
			},
		}, nil
	}

	if err := vp.validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	uid, _ := strconv.Atoi(vp.UID)
	gid, _ := strconv.Atoi(vp.GID)

	start := time.Now()
	volResp, err := client.CreateVolume(ctx, agentAPI.VolumeCreateRequest{
		Name:        req.Name,
		SizeBytes:   sizeBytes,
		NoCOW:       vp.NoCOW == "true",
		Compression: vp.Compression,
		UID:         uid,
		GID:         gid,
		Mode:        vp.Mode,
	})
	agentDuration.WithLabelValues("create_volume", sc).Observe(time.Since(start).Seconds())
	if err != nil {
		if agentAPI.IsConflict(err) {
			agentOpsTotal.WithLabelValues("create_volume", "conflict", sc).Inc()
			if volResp == nil {
				volResp, err = client.GetVolume(ctx, req.Name)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "volume conflict but failed to retrieve: %v", err)
				}
			}
		} else {
			agentOpsTotal.WithLabelValues("create_volume", "error", sc).Inc()
			return nil, status.Errorf(codes.Internal, "create volume: %v", err)
		}
	} else {
		agentOpsTotal.WithLabelValues("create_volume", "success", sc).Inc()
	}
	volCtx[model.ParamNFSSharePath] = volResp.Path

	log.Info().Str("volume", req.Name).Uint64("size", sizeBytes).Msg("volume created")

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      makeVolumeID(sc, req.Name),
			CapacityBytes: int64(sizeBytes),
			VolumeContext: volCtx,
		},
	}, nil
}

func (s *Server) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID required")
	}

	sc, name, err := parseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	deleteErr := client.DeleteVolume(ctx, name)
	agentDuration.WithLabelValues("delete_volume", sc).Observe(time.Since(start).Seconds())
	if deleteErr != nil {
		if agentAPI.IsNotFound(deleteErr) {
			agentOpsTotal.WithLabelValues("delete_volume", "not_found", sc).Inc()
			return &csi.DeleteVolumeResponse{}, nil
		}
		agentOpsTotal.WithLabelValues("delete_volume", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "delete volume: %v", deleteErr)
	}
	agentOpsTotal.WithLabelValues("delete_volume", "success", sc).Inc()

	log.Info().Str("volume", name).Msg("volume deleted")

	return &csi.DeleteVolumeResponse{}, nil
}

func (s *Server) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID required")
	}

	sc, name, err := parseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	var sizeBytes uint64
	if req.CapacityRange != nil {
		if req.CapacityRange.RequiredBytes > 0 {
			sizeBytes = uint64(req.CapacityRange.RequiredBytes)
		} else if req.CapacityRange.LimitBytes > 0 {
			sizeBytes = uint64(req.CapacityRange.LimitBytes)
		}
	}
	if sizeBytes == 0 {
		return nil, status.Error(codes.InvalidArgument, "capacity range required")
	}

	update := agentAPI.VolumeUpdateRequest{SizeBytes: &sizeBytes}

	start := time.Now()
	_, updateErr := client.UpdateVolume(ctx, name, update)
	agentDuration.WithLabelValues("update_volume", sc).Observe(time.Since(start).Seconds())
	if updateErr != nil {
		agentOpsTotal.WithLabelValues("update_volume", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "update volume: %v", updateErr)
	}
	agentOpsTotal.WithLabelValues("update_volume", "success", sc).Inc()

	log.Info().Str("volume", name).Uint64("size", sizeBytes).Msg("volume expanded")

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         int64(sizeBytes),
		NodeExpansionRequired: false,
	}, nil
}
