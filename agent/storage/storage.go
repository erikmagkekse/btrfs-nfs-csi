package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"

	"github.com/rs/zerolog/log"
)

const (
	MetadataFile = "metadata.json"
	DataDir      = "data"
	SnapshotsDir = "snapshots"
)

// Storage encapsulates all btrfs volume, snapshot, and clone operations.
type Storage struct {
	basePath        string
	quotaEnabled    bool
	exporter        nfs.Exporter
	tenants         []string
	defaultDirMode  os.FileMode
	defaultDataMode string
}

func New(basePath string, quotaEnabled bool, exporter nfs.Exporter, tenants []string, dirMode, dataMode string) *Storage {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	parsedDirMode, err := strconv.ParseUint(dirMode, 8, 32)
	if err != nil {
		log.Fatal().Str("mode", dirMode).Msg("invalid dir mode")
	}
	if _, err := strconv.ParseUint(dataMode, 8, 32); err != nil {
		log.Fatal().Str("mode", dataMode).Msg("invalid data mode")
	}

	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		log.Fatal().Str("path", basePath).Msg("base path does not exist or is not a directory")
	}
	if !btrfs.IsBtrfs(basePath) {
		log.Fatal().Str("path", basePath).Msg("base path is not on a btrfs filesystem")
	}
	if !btrfs.IsAvailable(ctx) {
		log.Fatal().Msg("btrfs tools not found - is btrfs-progs installed?")
	}
	if exporter == nil {
		log.Fatal().Msg("exporter must not be nil")
	}

	if quotaEnabled {
		if err := btrfs.QuotaCheck(ctx, basePath); err != nil {
			log.Fatal().Str("path", basePath).Msg("AGENT_FEATURE_QUOTA_ENABLED=true but btrfs quota is not enabled (run: btrfs quota enable " + basePath + ")")
		}
	}

	for _, name := range tenants {
		if err := validateName(name); err != nil {
			panic("storage: invalid tenant name: " + name)
		}
		td := filepath.Join(basePath, name)
		if err := os.MkdirAll(td, os.FileMode(parsedDirMode)); err != nil {
			panic("storage: failed to create tenant directory: " + err.Error())
		}
		if err := os.MkdirAll(filepath.Join(td, SnapshotsDir), os.FileMode(parsedDirMode)); err != nil {
			panic("storage: failed to create tenant snapshots directory: " + err.Error())
		}
	}
	log.Info().Int("count", len(tenants)).Msg("tenants configured")

	return &Storage{basePath: basePath, quotaEnabled: quotaEnabled, exporter: exporter, tenants: tenants, defaultDirMode: os.FileMode(parsedDirMode), defaultDataMode: dataMode}
}

func (s *Storage) StartWorkers(ctx context.Context, usageInterval, reconcileInterval time.Duration) {
	for _, tenant := range s.tenants {
		bp := filepath.Join(s.basePath, tenant)
		if s.quotaEnabled {
			StartUsageUpdater(ctx, bp, usageInterval, tenant)
		}
		if reconcileInterval > 0 {
			s.StartNFSReconciler(ctx, bp, reconcileInterval, tenant)
		}
	}
}

func (s *Storage) BasePath() string       { return s.basePath }
func (s *Storage) QuotaEnabled() bool     { return s.quotaEnabled }
func (s *Storage) Exporter() nfs.Exporter { return s.exporter }

func (s *Storage) tenantPath(tenant string) (string, error) {
	if err := validateName(tenant); err != nil {
		return "", err
	}
	bp := filepath.Join(s.basePath, tenant)
	if _, err := os.Stat(bp); os.IsNotExist(err) {
		return "", &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("tenant %q not found", tenant)}
	}
	return bp, nil
}

// --- Volume operations ---

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
	if !isValidCompression(req.Compression) {
		return nil, &StorageError{Code: ErrInvalid, Message: "compression must be one of: zstd, lzo, zlib, none"}
	}
	if req.QuotaBytes == 0 {
		req.QuotaBytes = req.SizeBytes
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
	dataDir := filepath.Join(volDir, DataDir)

	if _, err := os.Stat(volDir); err == nil {
		var existing VolumeMetadata
		if err := ReadMetadata(filepath.Join(volDir, MetadataFile), &existing); err != nil {
			return nil, fmt.Errorf("volume %q exists but metadata is corrupt: %w", req.Name, err)
		}
		return &existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("volume %q already exists", req.Name)}
	}

	if err := os.MkdirAll(volDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Str("path", volDir).Msg("failed to create volume directory")
		return nil, fmt.Errorf("create volume directory: %w", err)
	}

	cleanup := func() {
		_ = btrfs.SubvolumeDelete(ctx, dataDir)
		_ = os.RemoveAll(volDir)
	}

	if err := btrfs.SubvolumeCreate(ctx, dataDir); err != nil {
		_ = os.RemoveAll(volDir)
		log.Error().Err(err).Str("path", dataDir).Msg("failed to create subvolume")
		return nil, fmt.Errorf("btrfs subvolume create failed: %w", err)
	}

	if req.NoCOW {
		if err := btrfs.SetNoCOW(ctx, dataDir); err != nil {
			log.Error().Err(err).Str("path", dataDir).Msg("failed to set nocow")
			cleanup()
			return nil, fmt.Errorf("chattr +C failed: %w", err)
		}
	}

	if req.Compression != "" && req.Compression != "none" {
		if err := btrfs.SetCompression(ctx, dataDir, req.Compression); err != nil {
			log.Error().Err(err).Str("path", dataDir).Str("algo", req.Compression).Msg("failed to set compression")
			cleanup()
			return nil, fmt.Errorf("set compression failed: %w", err)
		}
	}

	if s.quotaEnabled {
		if err := btrfs.QgroupLimit(ctx, dataDir, req.QuotaBytes); err != nil {
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
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := writeMetadataAtomic(filepath.Join(volDir, MetadataFile), meta); err != nil {
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
		if !e.IsDir() || e.Name() == SnapshotsDir {
			continue
		}
		metaPath := filepath.Join(bp, e.Name(), MetadataFile)
		var meta VolumeMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}
		vols = append(vols, meta)
	}
	log.Debug().Str("tenant", tenant).Int("count", len(vols)).Msg("volumes listed")
	return vols, nil
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
	metaPath := filepath.Join(volDir, MetadataFile)
	dataDir := filepath.Join(volDir, DataDir)

	var cur VolumeMetadata
	if err := ReadMetadata(metaPath, &cur); err != nil {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
	}

	// validation
	if req.SizeBytes != nil && *req.SizeBytes <= cur.SizeBytes {
		return nil, &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("new size %d must be larger than current size %d", *req.SizeBytes, cur.SizeBytes)}
	}
	if req.Compression != nil {
		if !isValidCompression(*req.Compression) {
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
		if err := btrfs.QgroupLimit(ctx, dataDir, *req.SizeBytes); err != nil {
			log.Error().Err(err).Msg("failed to update qgroup limit")
			return nil, fmt.Errorf("qgroup limit failed: %w", err)
		}
	}

	if req.NoCOW != nil && *req.NoCOW && !cur.NoCOW {
		if err := btrfs.SetNoCOW(ctx, dataDir); err != nil {
			log.Error().Err(err).Msg("failed to set nocow")
			return nil, fmt.Errorf("chattr +C failed: %w", err)
		}
	} else if req.NoCOW != nil && !*req.NoCOW && cur.NoCOW {
		log.Warn().Str("volume", name).Msg("nocow cannot be reverted, ignoring")
		req.NoCOW = nil
	}

	if req.Compression != nil && *req.Compression != "" && *req.Compression != "none" {
		if err := btrfs.SetCompression(ctx, dataDir, *req.Compression); err != nil {
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
		meta.UpdatedAt = time.Now().UTC()
		updated = *meta
	}); err != nil {
		log.Error().Err(err).Msg("failed to update metadata")
		return nil, fmt.Errorf("failed to update metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Msg("volume updated")
	return &updated, nil
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

	if err := s.exporter.Unexport(ctx, volDir, ""); err != nil {
		log.Warn().Err(err).Str("path", volDir).Msg("failed to unexport via NFS")
	}

	dataDir := filepath.Join(volDir, DataDir)
	if err := btrfs.SubvolumeDelete(ctx, dataDir); err != nil {
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

func (s *Storage) ExportVolume(ctx context.Context, tenant, name, client string) error {
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

	// metadata first - if export fails, reconciler will re-export
	metaPath := filepath.Join(volDir, MetadataFile)
	if err := UpdateMetadata(metaPath, func(meta *VolumeMetadata) {
		found := false
		for _, c := range meta.Clients {
			if c == client {
				found = true
				break
			}
		}
		now := time.Now().UTC()
		meta.LastAttachAt = &now
		meta.UpdatedAt = now
		if !found {
			meta.Clients = append(meta.Clients, client)
		}
	}); err != nil {
		log.Error().Err(err).Msg("failed to persist client in metadata")
		return fmt.Errorf("failed to persist client in metadata: %w", err)
	}

	if err := s.exporter.Export(ctx, volDir, client); err != nil {
		log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to export, reconciler will retry")
		return fmt.Errorf("nfs export failed: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Str("client", client).Msg("NFS export added")
	return nil
}

func (s *Storage) UnexportVolume(ctx context.Context, tenant, name, client string) error {
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

	// metadata first - if unexport fails, reconciler will clean up
	metaPath := filepath.Join(volDir, MetadataFile)
	if err := UpdateMetadata(metaPath, func(meta *VolumeMetadata) {
		filtered := meta.Clients[:0]
		for _, c := range meta.Clients {
			if c != client {
				filtered = append(filtered, c)
			}
		}
		meta.Clients = filtered
		meta.UpdatedAt = time.Now().UTC()
	}); err != nil {
		log.Error().Err(err).Msg("failed to update client list in metadata")
		return fmt.Errorf("failed to update client list in metadata: %w", err)
	}

	if err := s.exporter.Unexport(ctx, volDir, client); err != nil {
		log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to unexport, reconciler will clean up")
		return fmt.Errorf("nfs unexport failed: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Str("client", client).Msg("NFS export removed")
	return nil
}

func (s *Storage) ListExports(ctx context.Context, tenant string) ([]ExportEntry, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	exports, err := s.exporter.ListExports(ctx)
	if err != nil {
		return nil, fmt.Errorf("list exports failed: %w", err)
	}

	var entries []ExportEntry
	for _, e := range exports {
		if strings.HasPrefix(e.Path, bp+"/") {
			entries = append(entries, ExportEntry{Path: e.Path, Client: e.Client})
		}
	}
	log.Debug().Str("tenant", tenant).Int("count", len(entries)).Msg("exports listed")
	return entries, nil
}

// --- Stats ---

type FsStats struct {
	TotalBytes uint64
	UsedBytes  uint64
	FreeBytes  uint64
}

func (s *Storage) Stats(tenant string) (*FsStats, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs(bp, &st); err != nil {
		return nil, fmt.Errorf("statfs failed: %w", err)
	}

	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)

	return &FsStats{
		TotalBytes: total,
		UsedBytes:  total - free,
		FreeBytes:  free,
	}, nil
}

// --- Snapshot operations ---

func (s *Storage) CreateSnapshot(ctx context.Context, tenant string, req SnapshotCreateRequest) (*SnapshotMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	// validation
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if err := validateName(req.Volume); err != nil {
		return nil, err
	}
	volDir := filepath.Join(bp, req.Volume)
	srcData := filepath.Join(volDir, DataDir)
	if _, err := os.Stat(srcData); os.IsNotExist(err) {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source volume %q not found", req.Volume)}
	}
	var volMeta VolumeMetadata
	if err := ReadMetadata(filepath.Join(volDir, MetadataFile), &volMeta); err != nil {
		return nil, fmt.Errorf("read volume metadata: %w", err)
	}

	snapDir := filepath.Join(bp, SnapshotsDir, req.Name)
	if _, err := os.Stat(snapDir); err == nil {
		return nil, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("snapshot %q already exists", req.Name)}
	}

	// operations
	if err := os.MkdirAll(snapDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create snapshot directory")
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	dstData := filepath.Join(snapDir, DataDir)
	if err := btrfs.SubvolumeSnapshot(ctx, srcData, dstData, true); err != nil {
		_ = os.RemoveAll(snapDir)
		log.Error().Err(err).Msg("failed to create snapshot")
		return nil, fmt.Errorf("btrfs snapshot failed: %w", err)
	}

	now := time.Now().UTC()
	meta := SnapshotMetadata{
		Name:      req.Name,
		Volume:    req.Volume,
		Path:      filepath.Join(filepath.Dir(volMeta.Path), SnapshotsDir, req.Name),
		SizeBytes: volMeta.SizeBytes,
		ReadOnly:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := writeMetadataAtomic(filepath.Join(snapDir, MetadataFile), meta); err != nil {
		log.Error().Err(err).Msg("failed to write snapshot metadata")
		_ = btrfs.SubvolumeDelete(ctx, dstData)
		_ = os.RemoveAll(snapDir)
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("volume", req.Volume).Msg("snapshot created")
	return &meta, nil
}

func (s *Storage) ListSnapshots(tenant, volume string) ([]SnapshotMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	snapBaseDir := filepath.Join(bp, SnapshotsDir)
	entries, err := os.ReadDir(snapBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		log.Error().Err(err).Msg("failed to read snapshots directory")
		return nil, fmt.Errorf("failed to read snapshots directory: %w", err)
	}

	var snaps []SnapshotMetadata
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(snapBaseDir, e.Name(), MetadataFile)
		var meta SnapshotMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}
		if volume != "" && meta.Volume != volume {
			continue
		}
		snaps = append(snaps, meta)
	}
	log.Debug().Str("tenant", tenant).Int("count", len(snaps)).Msg("snapshots listed")
	return snaps, nil
}

func (s *Storage) DeleteSnapshot(ctx context.Context, tenant, name string) error {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}

	snapDir := filepath.Join(bp, SnapshotsDir, name)
	if _, err := os.Stat(snapDir); os.IsNotExist(err) {
		return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("snapshot %q not found", name)}
	}

	dataDir := filepath.Join(snapDir, DataDir)
	if err := btrfs.SubvolumeDelete(ctx, dataDir); err != nil {
		log.Error().Err(err).Msg("failed to delete snapshot subvolume")
		return fmt.Errorf("btrfs subvolume delete failed: %w", err)
	}

	if err := os.RemoveAll(snapDir); err != nil {
		log.Error().Err(err).Msg("failed to remove snapshot directory")
		return fmt.Errorf("failed to remove snapshot directory: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Msg("snapshot deleted")
	return nil
}

// --- Clone operations ---

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
	snapDir := filepath.Join(bp, SnapshotsDir, req.Snapshot)
	srcData := filepath.Join(snapDir, DataDir)
	if _, err := os.Stat(srcData); os.IsNotExist(err) {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source snapshot %q not found", req.Snapshot)}
	}
	cloneDir := filepath.Join(bp, req.Name)
	if _, err := os.Stat(cloneDir); err == nil {
		var existing CloneMetadata
		if err := ReadMetadata(filepath.Join(cloneDir, MetadataFile), &existing); err != nil {
			return nil, fmt.Errorf("clone %q exists but metadata is corrupt: %w", req.Name, err)
		}
		return &existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("clone %q already exists", req.Name)}
	}

	// operations
	if err := os.MkdirAll(cloneDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create clone directory")
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	dstData := filepath.Join(cloneDir, DataDir)
	if err := btrfs.SubvolumeSnapshot(ctx, srcData, dstData, false); err != nil {
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

	if err := writeMetadataAtomic(filepath.Join(cloneDir, MetadataFile), meta); err != nil {
		log.Error().Err(err).Msg("failed to write clone metadata")
		_ = btrfs.SubvolumeDelete(ctx, dstData)
		_ = os.RemoveAll(cloneDir)
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("snapshot", req.Snapshot).Msg("clone created")
	return &meta, nil
}
