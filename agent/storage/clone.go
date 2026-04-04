package storage

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateClone(ctx context.Context, tenant string, req CloneCreateRequest) (*VolumeMetadata, error) {
	if _, err := s.tenantPath(tenant); err != nil {
		return nil, err
	}

	// validation
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if err := validateName(req.Snapshot); err != nil {
		return nil, err
	}
	labels := req.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[config.LabelCloneSourceType] = "snapshot"
	labels[config.LabelCloneSourceName] = req.Snapshot
	if err := validateLabels(labels); err != nil {
		return nil, err
	}
	if err := requireImmutableLabels(s.immutableLabelKeys, labels); err != nil {
		return nil, err
	}
	srcData := s.snapshots.DataPath(tenant, req.Snapshot)
	if !s.snapshots.Exists(tenant, req.Snapshot) {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source snapshot %q not found", req.Snapshot)}
	}
	cloneDir := s.volumes.Dir(tenant, req.Name)
	if existing, err := s.volumes.Get(tenant, req.Name); err == nil {
		return existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("clone %q already exists", req.Name)}
	}

	// operations
	if err := os.MkdirAll(cloneDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create clone directory")
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	dstData := s.volumes.DataPath(tenant, req.Name)
	if err := s.btrfs.SubvolumeSnapshot(ctx, srcData, dstData, false); err != nil {
		_ = os.RemoveAll(cloneDir)
		log.Error().Err(err).Msg("failed to create clone")
		return nil, fmt.Errorf("btrfs snapshot failed: %w", err)
	}

	now := time.Now().UTC()
	vol := VolumeMetadata{
		Name:      req.Name,
		Path:      cloneDir,
		Labels:    labels,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.volumes.Store(tenant, req.Name, &vol); err != nil {
		log.Error().Err(err).Msg("failed to write clone metadata")
		if delErr := s.btrfs.SubvolumeDelete(ctx, dstData); delErr != nil {
			log.Warn().Err(delErr).Str("path", dstData).Msg("cleanup: failed to delete subvolume")
		}
		_ = os.RemoveAll(cloneDir)
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("snapshot", req.Snapshot).Msg("clone created")
	return &vol, nil
}
