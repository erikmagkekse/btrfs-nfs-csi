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
	if err := config.ValidateSubvolumeName(req.Name); err != nil {
		return nil, err
	}
	if err := config.ValidateName(req.Snapshot); err != nil {
		return nil, err
	}
	labels := req.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[config.LabelCloneSourceType] = "snapshot"
	labels[config.LabelCloneSourceName] = req.Snapshot
	if err := config.ValidateLabels(labels); err != nil {
		return nil, err
	}
	if err := requireImmutableLabels(s.immutableLabelKeys, labels); err != nil {
		return nil, err
	}
	srcData, err := s.snapshots.DataPath(tenant, req.Snapshot)
	if err != nil {
		return nil, &StorageError{Code: ErrInvalid, Message: err.Error()}
	}
	snap, snapErr := s.snapshots.Get(tenant, req.Snapshot)
	if snapErr != nil {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source snapshot %q not found", req.Snapshot)}
	}
	srcVol, volErr := s.volumes.Get(tenant, snap.Volume)
	if volErr != nil {
		// Source volume was deleted. Fall back to snapshot properties.
		srcVol = &VolumeMetadata{
			SizeBytes:   snap.SizeBytes,
			QuotaBytes:  snap.QuotaBytes,
			NoCOW:       snap.NoCOW,
			Compression: snap.Compression,
			UID:         snap.UID,
			GID:         snap.GID,
			Mode:        snap.Mode,
		}
	}
	cloneDir, err := s.volumes.Dir(tenant, req.Name)
	if err != nil {
		return nil, &StorageError{Code: ErrInvalid, Message: err.Error()}
	}

	// Serialize concurrent creators of the same name (see CreateVolume).
	unlock, err := s.volumes.Lock(ctx, tenant, req.Name)
	if err != nil {
		return nil, &StorageError{Code: ErrBusy, Message: fmt.Sprintf("lock contention for clone %q: %v", req.Name, err)}
	}
	defer unlock()

	if existing, err := s.volumes.Get(tenant, req.Name); err == nil {
		return existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("clone %q already exists", req.Name)}
	}

	// operations
	if err := os.MkdirAll(cloneDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create clone directory")
		return nil, &StorageError{Code: ErrInternal, Message: fmt.Sprintf("failed to create clone directory: %v", err)}
	}

	dstData, err := s.volumes.DataPath(tenant, req.Name)
	if err != nil {
		return nil, &StorageError{Code: ErrInvalid, Message: err.Error()}
	}
	if err := s.btrfs.SubvolumeSnapshot(ctx, srcData, dstData, false); err != nil {
		if isSubvolumeAlreadyExistsError(err) {
			log.Warn().Err(err).Str("path", dstData).Msg("clone target already exists on disk")
			return nil, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("clone %q already exists on disk", req.Name)}
		}
		if rmErr := os.RemoveAll(cloneDir); rmErr != nil {
			log.Warn().Err(rmErr).Str("path", cloneDir).Msg("cleanup: failed to remove directory")
		}
		log.Error().Err(err).Msg("failed to create clone")
		return nil, &StorageError{Code: ErrInternal, Message: fmt.Sprintf("btrfs snapshot failed: %v", err)}
	}

	cleanup := func() {
		if err := s.cleanupSubvolume(ctx, dstData, cloneDir); err != nil {
			log.Warn().Err(err).Str("path", cloneDir).Msg("cleanup after failed create")
		}
	}

	uuid, err := s.btrfs.SubvolumeUUID(ctx, dstData)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read subvolume uuid: %w", err)
	}

	if s.quotaEnabled && srcVol.QuotaBytes > 0 {
		if err := s.btrfs.QgroupLimit(ctx, dstData, srcVol.QuotaBytes); err != nil {
			log.Error().Err(err).Str("path", dstData).Msg("failed to set qgroup limit on clone")
			cleanup()
			return nil, fmt.Errorf("qgroup limit failed: %w", err)
		}
	}

	now := time.Now().UTC()
	vol := VolumeMetadata{
		Name:        req.Name,
		Path:        cloneDir,
		UUID:        uuid,
		SizeBytes:   srcVol.SizeBytes,
		QuotaBytes:  srcVol.QuotaBytes,
		Compression: srcVol.Compression,
		NoCOW:       srcVol.NoCOW,
		UID:         srcVol.UID,
		GID:         srcVol.GID,
		Mode:        srcVol.Mode,
		Labels:      labels,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.volumes.Store(tenant, req.Name, &vol); err != nil {
		log.Error().Err(err).Msg("failed to write clone metadata")
		cleanup()
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Ctx(ctx).Info().Str("name", req.Name).Str("snapshot", req.Snapshot).Msg("clone created")
	return &vol, nil
}
