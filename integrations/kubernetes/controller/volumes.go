package controller

import (
	"context"
	"sort"
	"time"

	agentclient "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/client"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/csiserver"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/utils/ptr"
)

type agentEntry struct {
	sc     string
	client *agentclient.Client
}

func sortedAgents(agents map[string]*agentclient.Client) []agentEntry {
	entries := make([]agentEntry, 0, len(agents))
	for sc, c := range agents {
		entries = append(entries, agentEntry{sc: sc, client: c})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].sc < entries[j].sc })
	return entries
}

func (s *Server) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	pt, err := decodePageToken(req.StartingToken)
	if err != nil {
		return nil, err
	}

	agents := sortedAgents(s.agents.Agents())
	limit := int(req.MaxEntries)

	var entries []*csi.ListVolumesResponse_Entry
	var nextToken string

	for _, a := range agents {
		if pt.SC != "" && a.sc < pt.SC {
			continue
		}

		after := ""
		if a.sc == pt.SC {
			after = pt.After
		}

		opts := models.ListOpts{After: after, Limit: limit}
		start := time.Now()
		volList, err := a.client.ListVolumes(ctx, opts, nil)
		agentDuration.WithLabelValues("list_volumes", a.sc).Observe(time.Since(start).Seconds())
		if err != nil {
			agentOpsTotal.WithLabelValues("list_volumes", "error", a.sc).Inc()
			log.Warn().Err(err).Str("sc", a.sc).Msg("failed to list volumes from agent")
			continue
		}
		agentOpsTotal.WithLabelValues("list_volumes", "success", a.sc).Inc()

		for _, vol := range volList.Volumes {
			entries = append(entries, &csi.ListVolumesResponse_Entry{
				Volume: &csi.Volume{
					VolumeId:      csiserver.MakeVolumeID(a.sc, vol.Name),
					CapacityBytes: int64(vol.SizeBytes),
				},
			})
		}

		if volList.Next != "" {
			nextToken = encodePageToken(a.sc, volList.Next)
			break
		}

		if limit > 0 {
			limit -= len(volList.Volumes)
			if limit <= 0 {
				break
			}
		}
	}

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil
}

func (s *Server) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name required")
	}

	params := req.Parameters
	nfsServer := params[csiserver.ParamNFSServer]
	agentURL := params[paramAgentURL]
	if nfsServer == "" || agentURL == "" {
		return nil, status.Error(codes.InvalidArgument, "nfsServer and agentURL parameters required")
	}

	client, err := agentClientFromSecrets(agentURL, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("volume", req.Name).Str("agent", agentURL).Msg("failed to create agent client")
		return nil, err
	}

	s.agents.Track(agentURL, client)

	vp, err := s.resolveVolumeParams(ctx, params)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	sc := vp.StorageClass
	if sc == "" {
		return nil, status.Errorf(codes.Internal, "failed to resolve StorageClass name for volume %s", req.Name)
	}

	var sizeBytes uint64 = 1 << 30 // 1 GiB default
	if req.CapacityRange != nil {
		if req.CapacityRange.RequiredBytes > 0 {
			sizeBytes = uint64(req.CapacityRange.RequiredBytes)
		} else if req.CapacityRange.LimitBytes > 0 {
			sizeBytes = uint64(req.CapacityRange.LimitBytes)
		}
	}

	volCtx := map[string]string{
		csiserver.ParamNFSServer: nfsServer,
		paramAgentURL:            agentURL,
	}
	if opts := params[csiserver.ParamNFSMountOptions]; opts != "" {
		volCtx[csiserver.ParamNFSMountOptions] = opts
	}
	if n := params[csiserver.PvcNameKey]; n != "" {
		volCtx[csiserver.PvcNameKey] = n
	}
	if ns := params[csiserver.PvcNamespaceKey]; ns != "" {
		volCtx[csiserver.PvcNamespaceKey] = ns
	}

	// Clone from volume or snapshot
	if req.VolumeContentSource != nil {
		// PVC-to-PVC clone
		if srcVol := req.VolumeContentSource.GetVolume(); srcVol != nil {
			_, srcName, err := csiserver.ParseVolumeID(srcVol.VolumeId)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "invalid source volume ID: %v", err)
			}

			log.Debug().Str("volume", req.Name).Str("source", srcName).Str("sc", sc).Str("agent", agentURL).Msg("cloning volume from volume")

			start := time.Now()
			cloneResp, err := client.CloneVolume(ctx, models.VolumeCloneRequest{
				Source: srcName,
				Name:   req.Name,
				Labels: vp.Labels,
			})
			agentDuration.WithLabelValues("clone_volume", sc).Observe(time.Since(start).Seconds())
			if err != nil {
				if models.IsConflict(err) {
					agentOpsTotal.WithLabelValues("clone_volume", "conflict", sc).Inc()
					if cloneResp == nil {
						return nil, status.Errorf(codes.Internal, "clone conflict for volume %s but no metadata returned: %v", req.Name, err)
					}
				} else {
					agentOpsTotal.WithLabelValues("clone_volume", "error", sc).Inc()
					return nil, status.Errorf(codes.Internal, "clone volume %s from %s via %s: %v", req.Name, srcName, agentURL, err)
				}
			} else {
				agentOpsTotal.WithLabelValues("clone_volume", "success", sc).Inc()
			}
			volCtx[csiserver.ParamNFSSharePath] = cloneResp.Path

			if cloneResp.SizeBytes < sizeBytes {
				expandSize := sizeBytes
				if _, err := client.UpdateVolume(ctx, req.Name, models.VolumeUpdateRequest{SizeBytes: &expandSize}); err != nil {
					log.Warn().Err(err).Str("volume", req.Name).Uint64("from", cloneResp.SizeBytes).Uint64("to", sizeBytes).Msg("failed to expand clone")
				}
			}

			log.Info().Str("volume", req.Name).Str("source", srcName).Str("sc", sc).Str("agent", agentURL).Msg("volume cloned from volume")

			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      csiserver.MakeVolumeID(sc, req.Name),
					CapacityBytes: int64(sizeBytes),
					VolumeContext: volCtx,
					ContentSource: req.VolumeContentSource,
				},
			}, nil
		}

		// Clone from snapshot
		snap := req.VolumeContentSource.GetSnapshot()
		if snap == nil {
			return nil, status.Error(codes.InvalidArgument, "unsupported content source")
		}
		_, snapName, err := csiserver.ParseVolumeID(snap.SnapshotId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid snapshot ID: %v", err)
		}

		log.Debug().Str("volume", req.Name).Str("snapshot", snapName).Str("sc", sc).Str("agent", agentURL).Msg("cloning volume from snapshot")

		start := time.Now()
		cloneResp, err := client.CreateClone(ctx, models.CloneCreateRequest{
			Snapshot: snapName,
			Name:     req.Name,
			Labels:   vp.Labels,
		})
		agentDuration.WithLabelValues("create_clone", sc).Observe(time.Since(start).Seconds())
		if err != nil {
			if models.IsConflict(err) {
				agentOpsTotal.WithLabelValues("create_clone", "conflict", sc).Inc()
				if cloneResp == nil {
					return nil, status.Errorf(codes.Internal, "clone conflict for volume %s but no metadata returned: %v", req.Name, err)
				}
			} else {
				agentOpsTotal.WithLabelValues("create_clone", "error", sc).Inc()
				return nil, status.Errorf(codes.Internal, "clone volume %s from snapshot %s via %s: %v", req.Name, snapName, agentURL, err)
			}
		} else {
			agentOpsTotal.WithLabelValues("create_clone", "success", sc).Inc()
		}
		volCtx[csiserver.ParamNFSSharePath] = cloneResp.Path

		if cloneResp.SizeBytes < sizeBytes {
			expandSize := sizeBytes
			if _, err := client.UpdateVolume(ctx, req.Name, models.VolumeUpdateRequest{SizeBytes: &expandSize}); err != nil {
				log.Warn().Err(err).Str("volume", req.Name).Uint64("from", cloneResp.SizeBytes).Uint64("to", sizeBytes).Msg("failed to expand clone")
			}
		}

		log.Info().Str("volume", req.Name).Str("snapshot", snapName).Str("sc", sc).Str("agent", agentURL).Msg("volume cloned from snapshot")

		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				VolumeId:      csiserver.MakeVolumeID(sc, req.Name),
				CapacityBytes: int64(sizeBytes),
				VolumeContext: volCtx,
				ContentSource: req.VolumeContentSource,
			},
		}, nil
	}

	log.Debug().Str("volume", req.Name).Uint64("size", sizeBytes).Str("sc", sc).Str("agent", agentURL).Str("compression", ptr.Deref(vp.Compression, "")).Msg("creating volume")

	start := time.Now()
	volResp, err := client.CreateVolume(ctx, models.VolumeCreateRequest{
		Name:        req.Name,
		SizeBytes:   sizeBytes,
		NoCOW:       ptr.Deref(vp.NoCOW, false),
		Compression: ptr.Deref(vp.Compression, ""),
		UID:         ptr.Deref(vp.UID, 0),
		GID:         ptr.Deref(vp.GID, 0),
		Mode:        ptr.Deref(vp.Mode, ""),
		Labels:      vp.Labels,
	})
	agentDuration.WithLabelValues("create_volume", sc).Observe(time.Since(start).Seconds())
	if err != nil {
		if models.IsConflict(err) {
			agentOpsTotal.WithLabelValues("create_volume", "conflict", sc).Inc()
			if volResp == nil {
				volResp, err = client.GetVolume(ctx, req.Name)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "volume %s conflict but failed to retrieve from %s: %v", req.Name, agentURL, err)
				}
			}
		} else {
			agentOpsTotal.WithLabelValues("create_volume", "error", sc).Inc()
			return nil, status.Errorf(codes.Internal, "create volume %s via %s: %v", req.Name, agentURL, err)
		}
	} else {
		agentOpsTotal.WithLabelValues("create_volume", "success", sc).Inc()
	}
	volCtx[csiserver.ParamNFSSharePath] = volResp.Path

	log.Info().Str("volume", req.Name).Uint64("size", sizeBytes).Str("sc", sc).Str("agent", agentURL).Msg("volume created")

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      csiserver.MakeVolumeID(sc, req.Name),
			CapacityBytes: int64(sizeBytes),
			VolumeContext: volCtx,
		},
	}, nil
}

func (s *Server) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID required")
	}

	sc, name, err := csiserver.ParseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("volume", name).Str("sc", sc).Msg("failed to create agent client for delete")
		return nil, err
	}

	log.Debug().Str("volume", name).Str("sc", sc).Msg("deleting volume")

	start := time.Now()
	deleteErr := client.DeleteVolume(ctx, name)
	agentDuration.WithLabelValues("delete_volume", sc).Observe(time.Since(start).Seconds())
	if deleteErr != nil {
		if models.IsNotFound(deleteErr) {
			agentOpsTotal.WithLabelValues("delete_volume", "not_found", sc).Inc()
			log.Info().Str("volume", name).Str("sc", sc).Msg("volume already deleted")
			return &csi.DeleteVolumeResponse{}, nil
		}
		if models.IsLocked(deleteErr) {
			agentOpsTotal.WithLabelValues("delete_volume", "busy", sc).Inc()
			return nil, status.Errorf(codes.FailedPrecondition, "delete volume %s: %v", name, deleteErr)
		}
		agentOpsTotal.WithLabelValues("delete_volume", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "delete volume %s via %s: %v", name, sc, deleteErr)
	}
	agentOpsTotal.WithLabelValues("delete_volume", "success", sc).Inc()

	log.Info().Str("volume", name).Str("sc", sc).Msg("volume deleted")

	return &csi.DeleteVolumeResponse{}, nil
}

func (s *Server) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID required")
	}

	sc, name, err := csiserver.ParseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("volume", name).Str("sc", sc).Msg("failed to create agent client for expand")
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

	log.Debug().Str("volume", name).Uint64("size", sizeBytes).Str("sc", sc).Msg("expanding volume")

	update := models.VolumeUpdateRequest{SizeBytes: &sizeBytes}

	start := time.Now()
	_, updateErr := client.UpdateVolume(ctx, name, update)
	agentDuration.WithLabelValues("update_volume", sc).Observe(time.Since(start).Seconds())
	if updateErr != nil {
		agentOpsTotal.WithLabelValues("update_volume", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "expand volume %s via %s: %v", name, sc, updateErr)
	}
	agentOpsTotal.WithLabelValues("update_volume", "success", sc).Inc()

	log.Info().Str("volume", name).Uint64("size", sizeBytes).Str("sc", sc).Msg("volume expanded")

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         int64(sizeBytes),
		NodeExpansionRequired: false,
	}, nil
}
