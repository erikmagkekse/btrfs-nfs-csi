package controller

import (
	"context"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.VolumeId == "" || req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and node ID required")
	}

	nodeIP, err := parseNodeIP(req.NodeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	sc, name, err := parseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	// apply PVC annotation changes to agent
	vp := resolveVolumeParams(ctx, req.VolumeContext)
	if err := vp.validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if update, changed := vp.toUpdateRequest(); changed {
		start := time.Now()
		_, updateErr := client.UpdateVolume(ctx, name, update)
		agentDuration.WithLabelValues("update_volume", sc).Observe(time.Since(start).Seconds())
		if updateErr != nil {
			agentOpsTotal.WithLabelValues("update_volume", "error", sc).Inc()
			log.Warn().Err(updateErr).Str("volume", name).Msg("failed to apply annotation updates")
		} else {
			agentOpsTotal.WithLabelValues("update_volume", "success", sc).Inc()
		}
	}

	// retry export - block on failure so the node doesn't attempt mounting a non-existent export
	var exportErr error
	for attempt := 0; attempt < 3; attempt++ {
		retryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		start := time.Now()
		exportErr = client.ExportVolume(retryCtx, name, nodeIP)
		agentDuration.WithLabelValues("export", sc).Observe(time.Since(start).Seconds())
		cancel()
		if exportErr == nil {
			agentOpsTotal.WithLabelValues("export", "success", sc).Inc()
			break
		}
		agentOpsTotal.WithLabelValues("export", "error", sc).Inc()
		log.Warn().Err(exportErr).Int("attempt", attempt+1).Str("volume", name).Str("nodeIP", nodeIP).Msg("nfs export failed, retrying")
	}
	if exportErr != nil {
		return nil, status.Errorf(codes.Internal, "nfs export for node %s after 3 attempts: %v", nodeIP, exportErr)
	}

	log.Info().Str("volume", name).Str("nodeIP", nodeIP).Msg("nfs export added")

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (s *Server) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.VolumeId == "" || req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID and node ID required")
	}

	nodeIP, err := parseNodeIP(req.NodeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	sc, name, err := parseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	// retry unexport - block on failure to preserve VolumeAttachment so k8s knows
	// the node still holds the volume and can schedule pods accordingly
	var unexportErr error
	for attempt := 0; attempt < 3; attempt++ {
		retryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		start := time.Now()
		unexportErr = client.UnexportVolume(retryCtx, name, nodeIP)
		agentDuration.WithLabelValues("unexport", sc).Observe(time.Since(start).Seconds())
		cancel()
		if unexportErr == nil {
			agentOpsTotal.WithLabelValues("unexport", "success", sc).Inc()
			break
		}
		if agentAPI.IsNotFound(unexportErr) {
			agentOpsTotal.WithLabelValues("unexport", "not_found", sc).Inc()
			unexportErr = nil
			break
		}
		agentOpsTotal.WithLabelValues("unexport", "error", sc).Inc()
		log.Warn().Err(unexportErr).Int("attempt", attempt+1).Str("volume", name).Str("nodeIP", nodeIP).Msg("nfs unexport failed, retrying")
	}
	if unexportErr != nil {
		return nil, status.Errorf(codes.Internal, "nfs unexport for node %s after 3 attempts: %v", nodeIP, unexportErr)
	}

	log.Info().Str("volume", name).Str("nodeIP", nodeIP).Msg("nfs export removed")

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}
