package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateVolume(ctx context.Context, tenant string, req VolumeCreateRequest) (*VolumeMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	// validation
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if req.SizeBytes == 0 {
		return nil, &StorageError{Code: ErrInvalid, Message: "size_bytes is required"}
	}
	if req.NoCOW && req.Compression != "" && req.Compression != "none" {
		return nil, &StorageError{Code: ErrInvalid, Message: "nocow and compression are mutually exclusive"}
	}
	if !utils.IsValidCompression(req.Compression) {
		return nil, &StorageError{Code: ErrInvalid, Message: "compression must be one of: zstd, lzo, zlib, none"}
	}
	if req.QuotaBytes == 0 {
		req.QuotaBytes = req.SizeBytes
	}
	if err := validateLabels(req.Labels); err != nil {
		return nil, err
	}
	if req.Mode == "" {
		req.Mode = s.defaultDataMode
	}
	mode, err := strconv.ParseUint(req.Mode, 8, 32)
	if err != nil {
		return nil, &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid mode: %s", req.Mode)}
	}

	// operations
	volDir := filepath.Join(bp, req.Name)
	dataDir := filepath.Join(volDir, config.DataDir)

	if _, err := os.Stat(volDir); err == nil {
		var existing VolumeMetadata
		if err := ReadMetadata(filepath.Join(volDir, config.MetadataFile), &existing); err != nil {
			return nil, fmt.Errorf("volume %q exists but metadata is corrupt: %w", req.Name, err)
		}
		return &existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("volume %q already exists", req.Name)}
	}

	if err := os.MkdirAll(volDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Str("path", volDir).Msg("failed to create volume directory")
		return nil, fmt.Errorf("create volume directory: %w", err)
	}

	cleanup := func() {
		if err := s.btrfs.SubvolumeDelete(ctx, dataDir); err != nil {
			log.Warn().Err(err).Str("path", dataDir).Msg("cleanup: failed to delete subvolume")
		}
		if err := os.RemoveAll(volDir); err != nil {
			log.Warn().Err(err).Str("path", volDir).Msg("cleanup: failed to remove directory")
		}
	}

	if err := s.btrfs.SubvolumeCreate(ctx, dataDir); err != nil {
		_ = os.RemoveAll(volDir)
		log.Error().Err(err).Str("path", dataDir).Msg("failed to create subvolume")
		return nil, fmt.Errorf("btrfs subvolume create failed: %w", err)
	}

	if req.NoCOW {
		if err := s.btrfs.SetNoCOW(ctx, dataDir); err != nil {
			log.Error().Err(err).Str("path", dataDir).Msg("failed to set nocow")
			cleanup()
			return nil, fmt.Errorf("chattr +C failed: %w", err)
		}
	}

	if req.Compression != "" && req.Compression != "none" {
		if err := s.btrfs.SetCompression(ctx, dataDir, req.Compression); err != nil {
			log.Error().Err(err).Str("path", dataDir).Str("algo", req.Compression).Msg("failed to set compression")
			cleanup()
			return nil, fmt.Errorf("set compression failed: %w", err)
		}
	}

	if s.quotaEnabled {
		if err := s.btrfs.QgroupLimit(ctx, dataDir, req.QuotaBytes); err != nil {
			log.Error().Err(err).Str("path", dataDir).Uint64("bytes", req.QuotaBytes).Msg("failed to set qgroup limit")
			cleanup()
			return nil, fmt.Errorf("qgroup limit failed: %w", err)
		}
	}

	if err := os.Chmod(dataDir, fileMode(mode)); err != nil {
		log.Error().Err(err).Msg("failed to chmod")
	}
	if err := os.Chown(dataDir, req.UID, req.GID); err != nil {
		log.Error().Err(err).Msg("failed to chown")
	}

	now := time.Now().UTC()
	meta := VolumeMetadata{
		Name:        req.Name,
		Path:        volDir,
		SizeBytes:   req.SizeBytes,
		NoCOW:       req.NoCOW,
		Compression: req.Compression,
		QuotaBytes:  req.QuotaBytes,
		UID:         req.UID,
		GID:         req.GID,
		Mode:        req.Mode,
		Labels:      req.Labels,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := writeMetadataAtomic(filepath.Join(volDir, config.MetadataFile), meta); err != nil {
		log.Error().Err(err).Msg("failed to write metadata")
		cleanup()
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("path", volDir).Msg("volume created")
	return &meta, nil
}

func (s *Storage) ListVolumes(tenant string) ([]VolumeMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(bp)
	if err != nil {
		log.Error().Err(err).Msg("failed to read base path")
		return nil, fmt.Errorf("failed to read base path: %w", err)
	}

	var vols []VolumeMetadata
	for _, e := range entries {
		if !e.IsDir() || e.Name() == config.SnapshotsDir {
			continue
		}
		metaPath := filepath.Join(bp, e.Name(), config.MetadataFile)
		var meta VolumeMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}
		vols = append(vols, meta)
	}
	log.Debug().Str("tenant", tenant).Int("count", len(vols)).Msg("volumes listed")
	return vols, nil
}

func (s *Storage) GetVolume(tenant, name string) (*VolumeMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}

	metaPath := filepath.Join(bp, name, config.MetadataFile)
	var meta VolumeMetadata
	if err := ReadMetadata(metaPath, &meta); err != nil {
		if os.IsNotExist(err) {
			return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
		}
		return nil, &StorageError{Code: ErrMetadata, Message: fmt.Sprintf("volume %q: failed to read metadata: %v", name, err)}
	}
	return &meta, nil
}

func (s *Storage) UpdateVolume(ctx context.Context, tenant, name string, req VolumeUpdateRequest) (*VolumeMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}

	volDir := filepath.Join(bp, name)
	metaPath := filepath.Join(volDir, config.MetadataFile)
	dataDir := filepath.Join(volDir, config.DataDir)

	var cur VolumeMetadata
	if err := ReadMetadata(metaPath, &cur); err != nil {
		if os.IsNotExist(err) {
			return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
		}
		return nil, &StorageError{Code: ErrMetadata, Message: fmt.Sprintf("volume %q: failed to read metadata: %v", name, err)}
	}

	// validation
	if req.Labels != nil {
		if err := validateLabels(*req.Labels); err != nil {
			return nil, err
		}
	}
	if req.SizeBytes != nil && *req.SizeBytes <= cur.SizeBytes {
		return nil, &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("new size %d must be larger than current size %d", *req.SizeBytes, cur.SizeBytes)}
	}
	if req.Compression != nil {
		if !utils.IsValidCompression(*req.Compression) {
			return nil, &StorageError{Code: ErrInvalid, Message: "compression must be one of: zstd, lzo, zlib, none"}
		}
		if cur.NoCOW && *req.Compression != "" && *req.Compression != "none" {
			return nil, &StorageError{Code: ErrInvalid, Message: "nocow and compression are mutually exclusive"}
		}
	}
	var parsedMode uint64
	if req.Mode != nil {
		var err error
		parsedMode, err = strconv.ParseUint(*req.Mode, 8, 32)
		if err != nil {
			return nil, &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid mode: %s", *req.Mode)}
		}
	}

	// operations
	if req.SizeBytes != nil && s.quotaEnabled {
		if err := s.btrfs.QgroupLimit(ctx, dataDir, *req.SizeBytes); err != nil {
			log.Error().Err(err).Msg("failed to update qgroup limit")
			return nil, fmt.Errorf("qgroup limit failed: %w", err)
		}
	}

	if req.NoCOW != nil && *req.NoCOW && !cur.NoCOW {
		if err := s.btrfs.SetNoCOW(ctx, dataDir); err != nil {
			log.Error().Err(err).Msg("failed to set nocow")
			return nil, fmt.Errorf("chattr +C failed: %w", err)
		}
	} else if req.NoCOW != nil && !*req.NoCOW && cur.NoCOW {
		log.Warn().Str("volume", name).Msg("nocow cannot be reverted, ignoring")
		req.NoCOW = nil
	}

	if req.Compression != nil && *req.Compression != "" && *req.Compression != "none" {
		if err := s.btrfs.SetCompression(ctx, dataDir, *req.Compression); err != nil {
			log.Error().Err(err).Msg("failed to set compression")
			return nil, fmt.Errorf("set compression failed: %w", err)
		}
	}

	if req.UID != nil || req.GID != nil {
		uid := cur.UID
		gid := cur.GID
		if req.UID != nil {
			uid = *req.UID
		}
		if req.GID != nil {
			gid = *req.GID
		}
		if err := os.Chown(dataDir, uid, gid); err != nil {
			log.Error().Err(err).Msg("failed to chown")
			return nil, fmt.Errorf("chown failed: %w", err)
		}
	}

	if req.Mode != nil {
		if err := os.Chmod(dataDir, fileMode(parsedMode)); err != nil {
			log.Error().Err(err).Msg("failed to chmod")
			return nil, fmt.Errorf("chmod failed: %w", err)
		}
	}

	var updated VolumeMetadata
	if err := UpdateMetadata(metaPath, func(meta *VolumeMetadata) {
		if req.SizeBytes != nil {
			meta.SizeBytes = *req.SizeBytes
			meta.QuotaBytes = *req.SizeBytes
		}
		if req.NoCOW != nil {
			meta.NoCOW = *req.NoCOW
		}
		if req.Compression != nil {
			meta.Compression = *req.Compression
		}
		if req.UID != nil {
			meta.UID = *req.UID
		}
		if req.GID != nil {
			meta.GID = *req.GID
		}
		if req.Mode != nil {
			meta.Mode = *req.Mode
		}
		if req.Labels != nil {
			meta.Labels = *req.Labels
		}
		meta.UpdatedAt = time.Now().UTC()
		updated = *meta
	}); err != nil {
		log.Error().Err(err).Msg("failed to update metadata")
		return nil, fmt.Errorf("failed to update metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Msg("volume updated")
	return &updated, nil
}

func (s *Storage) CloneVolume(ctx context.Context, tenant string, req VolumeCloneRequest) (*VolumeMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}
	if err := validateName(req.Name); err != nil {
		return nil, err
	}

	if err := validateLabels(req.Labels); err != nil {
		return nil, err
	}

	src, err := s.GetVolume(tenant, req.Source)
	if err != nil {
		return nil, err
	}

	cloneDir := filepath.Join(bp, req.Name)
	if _, err := os.Stat(cloneDir); err == nil {
		var existing VolumeMetadata
		if err := ReadMetadata(filepath.Join(cloneDir, config.MetadataFile), &existing); err != nil {
			return nil, fmt.Errorf("volume %q exists but metadata is corrupt: %w", req.Name, err)
		}
		return &existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("volume %q already exists", req.Name)}
	}

	if err := os.MkdirAll(cloneDir, s.defaultDirMode); err != nil {
		return nil, fmt.Errorf("create clone directory: %w", err)
	}

	srcData := filepath.Join(bp, req.Source, config.DataDir)
	cloneData := filepath.Join(cloneDir, config.DataDir)

	cleanup := func() {
		if err := s.btrfs.SubvolumeDelete(ctx, cloneData); err != nil {
			log.Warn().Err(err).Str("path", cloneData).Msg("cleanup: failed to delete subvolume")
		}
		if err := os.RemoveAll(cloneDir); err != nil {
			log.Warn().Err(err).Str("path", cloneDir).Msg("cleanup: failed to remove directory")
		}
	}

	if err := s.btrfs.SubvolumeSnapshot(ctx, srcData, cloneData, false); err != nil {
		cleanup()
		return nil, fmt.Errorf("btrfs snapshot failed: %w", err)
	}

	if s.quotaEnabled {
		if err := s.btrfs.QgroupLimit(ctx, cloneData, src.QuotaBytes); err != nil {
			log.Error().Err(err).Str("path", cloneData).Msg("failed to set qgroup limit on clone")
			cleanup()
			return nil, fmt.Errorf("qgroup limit failed: %w", err)
		}
	}

	labels := req.Labels
	if labels == nil {
		labels = src.Labels
	}

	now := time.Now().UTC()
	meta := VolumeMetadata{
		Name:        req.Name,
		Path:        cloneDir,
		SizeBytes:   src.SizeBytes,
		NoCOW:       src.NoCOW,
		Compression: src.Compression,
		QuotaBytes:  src.QuotaBytes,
		UID:         src.UID,
		GID:         src.GID,
		Mode:        src.Mode,
		Labels:      labels,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := writeMetadataAtomic(filepath.Join(cloneDir, config.MetadataFile), meta); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("source", req.Source).Msg("volume cloned")
	return &meta, nil
}

func (s *Storage) DeleteVolume(ctx context.Context, tenant, name string) error {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}

	volDir := filepath.Join(bp, name)
	if _, err := os.Stat(volDir); os.IsNotExist(err) {
		return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
	}

	var meta VolumeMetadata
	metaPath := filepath.Join(volDir, config.MetadataFile)
	if err := ReadMetadata(metaPath, &meta); err != nil {
		return fmt.Errorf("failed to read volume metadata: %w", err)
	}
	if len(meta.Clients) > 0 {
		return &StorageError{Code: ErrBusy, Message: fmt.Sprintf("volume %q still has active NFS exports", name)}
	}

	dataDir := filepath.Join(volDir, config.DataDir)
	if err := s.btrfs.SubvolumeDelete(ctx, dataDir); err != nil {
		log.Error().Err(err).Msg("failed to delete subvolume")
		return fmt.Errorf("btrfs subvolume delete failed: %w", err)
	}

	if err := os.RemoveAll(volDir); err != nil {
		log.Error().Err(err).Msg("failed to remove volume directory")
		return fmt.Errorf("failed to remove volume directory: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Msg("volume deleted")
	return nil
}
