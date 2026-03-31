package controller

import (
	"context"
	"time"

	agentAPI "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

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

	nodeIP, err := parseNodeIP(req.NodeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	sc, name, err := utils.ParseVolumeID(req.VolumeId)
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

	exportCtx, cancel := context.WithTimeout(ctx, exportTimeout)
	defer cancel()
	start := time.Now()
	if err := client.ExportVolume(exportCtx, name, nodeIP); err != nil {
		agentDuration.WithLabelValues("export", sc).Observe(time.Since(start).Seconds())
		agentOpsTotal.WithLabelValues("export", "error", sc).Inc()
		return nil, status.Errorf(codes.Internal, "nfs export for node %s: %v", nodeIP, err)
	}
	agentDuration.WithLabelValues("export", sc).Observe(time.Since(start).Seconds())
	agentOpsTotal.WithLabelValues("export", "success", sc).Inc()

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

	sc, name, err := utils.ParseVolumeID(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	client, err := agentClientFromStorageClass(s.agents, sc, req.Secrets)
	if err != nil {
		return nil, err
	}

	unexportCtx, cancel2 := context.WithTimeout(ctx, exportTimeout)
	defer cancel2()
	start2 := time.Now()
	unexportErr := client.UnexportVolume(unexportCtx, name, nodeIP)
	agentDuration.WithLabelValues("unexport", sc).Observe(time.Since(start2).Seconds())
	if unexportErr != nil {
		if agentAPI.IsNotFound(unexportErr) {
			agentOpsTotal.WithLabelValues("unexport", "not_found", sc).Inc()
		} else {
			agentOpsTotal.WithLabelValues("unexport", "error", sc).Inc()
			return nil, status.Errorf(codes.Internal, "nfs unexport for node %s: %v", nodeIP, unexportErr)
		}
	} else {
		agentOpsTotal.WithLabelValues("unexport", "success", sc).Inc()
	}

	log.Info().Str("volume", name).Str("nodeIP", nodeIP).Msg("nfs export removed")

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}
