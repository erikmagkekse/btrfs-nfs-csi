package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateClone(ctx context.Context, tenant string, req CloneCreateRequest) (*CloneMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	// validation
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if err := validateName(req.Snapshot); err != nil {
		return nil, err
	}
	snapDir := filepath.Join(bp, config.SnapshotsDir, req.Snapshot)
	srcData := filepath.Join(snapDir, config.DataDir)
	if _, err := os.Stat(srcData); os.IsNotExist(err) {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source snapshot %q not found", req.Snapshot)}
	}
	cloneDir := filepath.Join(bp, req.Name)
	if _, err := os.Stat(cloneDir); err == nil {
		var existing CloneMetadata
		if err := ReadMetadata(filepath.Join(cloneDir, config.MetadataFile), &existing); err != nil {
			return nil, fmt.Errorf("clone %q exists but metadata is corrupt: %w", req.Name, err)
		}
		return &existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("clone %q already exists", req.Name)}
	}

	// operations
	if err := os.MkdirAll(cloneDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create clone directory")
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	dstData := filepath.Join(cloneDir, config.DataDir)
	if err := s.btrfs.SubvolumeSnapshot(ctx, srcData, dstData, false); err != nil {
		_ = os.RemoveAll(cloneDir)
		log.Error().Err(err).Msg("failed to create clone")
		return nil, fmt.Errorf("btrfs snapshot failed: %w", err)
	}

	now := time.Now().UTC()
	meta := CloneMetadata{
		Name:           req.Name,
		SourceSnapshot: req.Snapshot,
		Path:           cloneDir,
		CreatedAt:      now,
	}

	if err := writeMetadataAtomic(filepath.Join(cloneDir, config.MetadataFile), meta); err != nil {
		log.Error().Err(err).Msg("failed to write clone metadata")
		if delErr := s.btrfs.SubvolumeDelete(ctx, dstData); delErr != nil {
			log.Warn().Err(delErr).Str("path", dstData).Msg("cleanup: failed to delete subvolume")
		}
		_ = os.RemoveAll(cloneDir)
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("snapshot", req.Snapshot).Msg("clone created")
	return &meta, nil
}
