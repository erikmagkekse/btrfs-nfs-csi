package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/integrations/kubernetes/csiserver"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// exportTimeout is the context timeout for a single export/unexport call.
const exportTimeout = 10 * time.Second

func (s *Server) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.VolumeId == "" || req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and node ID required")
	}

	nodeHostname, nodeIP, err := csiserver.ParseNodeID(req.NodeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	sc, name, err := csiserver.ParseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("volume", name).Str("sc", sc).Msg("failed to create agent client for publish")
		return nil, err
	}

	// apply PVC annotation changes to agent
	vp, err := s.resolveVolumeParams(ctx, req.VolumeContext)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume %s: %v", name, err)
	}
	if vp.hasUpdates() {
		log.Debug().Str("volume", name).Str("sc", sc).Msg("applying PVC annotation updates to agent")
		start := time.Now()
		_, updateErr := client.UpdateVolume(ctx, name, vp.updateRequest())
		agentDuration.WithLabelValues("update_volume", sc).Observe(time.Since(start).Seconds())
		if updateErr != nil {
			agentOpsTotal.WithLabelValues("update_volume", "error", sc).Inc()
			log.Warn().Err(updateErr).Str("volume", name).Str("sc", sc).Msg("failed to apply PVC annotation updates")
		} else {
			agentOpsTotal.WithLabelValues("update_volume", "success", sc).Inc()
		}
	}

	log.Debug().Str("volume", name).Str("nodeIP", nodeIP).Str("sc", sc).Msg("exporting volume")

	exportCtx, cancel := context.WithTimeout(ctx, exportTimeout)
	defer cancel()
	start := time.Now()
	vaName := fmt.Sprintf("csi-%x", sha256.Sum256([]byte(req.VolumeId+csiserver.DriverName+nodeHostname)))
	exportLabels := map[string]string{
		labelNodeName:             nodeHostname,
		labelPVName:               name,
		labelPVStorageClass:       sc,
		labelVolumeAttachmentName: vaName,
	}
	if pvcName := req.VolumeContext[csiserver.PvcNameKey]; pvcName != "" {
		exportLabels[labelPVCName] = pvcName
	}
	if pvcNS := req.VolumeContext[csiserver.PvcNamespaceKey]; pvcNS != "" {
		exportLabels[labelPVCNamespace] = pvcNS
	}
	if err := client.CreateVolumeExport(exportCtx, name, nodeIP, exportLabels); err != nil {
		agentDuration.WithLabelValues("export", sc).Observe(time.Since(start).Seconds())
		agentOpsTotal.WithLabelValues("export", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "export volume %s to node %s via %s: %v", name, nodeIP, sc, err)
	}
	agentDuration.WithLabelValues("export", sc).Observe(time.Since(start).Seconds())
	agentOpsTotal.WithLabelValues("export", "success", sc).Inc()

	log.Info().Str("volume", name).Str("nodeIP", nodeIP).Str("sc", sc).Msg("publish complete")

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (s *Server) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.VolumeId == "" || req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and node ID required")
	}

	nodeHostname, nodeIP, err := csiserver.ParseNodeID(req.NodeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	sc, name, err := csiserver.ParseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		log.Error().Err(err).Str("volume", name).Str("sc", sc).Msg("failed to create agent client for unpublish")
		return nil, err
	}

	log.Debug().Str("volume", name).Str("nodeIP", nodeIP).Str("sc", sc).Msg("unexporting volume")

	unexportCtx, cancel2 := context.WithTimeout(ctx, exportTimeout)
	defer cancel2()
	start2 := time.Now()
	unexportLabels := map[string]string{
		labelNodeName:       nodeHostname,
		labelPVName:         name,
		labelPVStorageClass: sc,
	}
	unexportErr := client.DeleteVolumeExport(unexportCtx, name, nodeIP, unexportLabels)
	agentDuration.WithLabelValues("unexport", sc).Observe(time.Since(start2).Seconds())
	if unexportErr != nil {
		if models.IsNotFound(unexportErr) {
			agentOpsTotal.WithLabelValues("unexport", "not_found", sc).Inc()
			log.Info().Str("volume", name).Str("nodeIP", nodeIP).Str("sc", sc).Msg("export already removed")
		} else {
			agentOpsTotal.WithLabelValues("unexport", "error", sc).Inc()
			return nil, status.Errorf(codes.Internal, "unexport volume %s from node %s via %s: %v", name, nodeIP, sc, unexportErr)
		}
	} else {
		agentOpsTotal.WithLabelValues("unexport", "success", sc).Inc()
	}

	log.Info().Str("volume", name).Str("nodeIP", nodeIP).Str("sc", sc).Msg("unpublish complete")

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}
