package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	"github.com/rs/zerolog/log"
)

// Storage encapsulates all btrfs volume, snapshot, and clone operations.
type Storage struct {
	basePath        string
	mountPoint      string
	quotaEnabled    bool
	btrfs           *btrfs.Manager
	exporter        nfs.Exporter
	tenants         []string
	defaultDirMode  os.FileMode
	defaultDataMode string

	// cachedDevices is written by both the IO poller (5s) and btrfs stats poller (1m).
	// Each poller loads the current state, updates its own fields (IO or Errors),
	// and preserves the other poller's fields from the previous snapshot.
	// Uses atomic.Pointer instead of a mutex. Concurrent load+store from two pollers
	// may cause one update to be lost, but the next poll cycle self-corrects
	// (max 5s for IO, max 1m for errors).
	cachedDevices    atomic.Pointer[[]DeviceState]
	cachedFilesystem atomic.Pointer[btrfs.FilesystemUsage]
}

func New(basePath string, quotaEnabled bool, exporter nfs.Exporter, tenants []string, dirMode, dataMode, btrfsBin string) *Storage {
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
	mountPoint, err := utils.FindMountPoint(basePath)
	if err != nil {
		log.Fatal().Err(err).Str("path", basePath).Msg("failed to resolve btrfs mount point")
	}
	if mountPoint != basePath {
		log.Info().Str("basePath", basePath).Str("mountPoint", mountPoint).Msg("base path is a subdirectory of btrfs mount")
	}
	mgr := btrfs.NewManager(btrfsBin)
	if !mgr.IsAvailable(ctx) {
		log.Fatal().Msg("btrfs tools not found - is btrfs-progs installed?")
	}
	if exporter == nil {
		log.Fatal().Msg("exporter must not be nil")
	}

	if quotaEnabled {
		if err := mgr.QuotaCheck(ctx, basePath); err != nil {
			log.Fatal().Str("path", basePath).Msg("AGENT_FEATURE_QUOTA_ENABLED=true but btrfs quota is not enabled (run: btrfs quota enable " + basePath + ")")
		}
	}

	for _, name := range tenants {
		if err := validateName(name); err != nil {
			log.Fatal().Str("tenant", name).Msg("invalid tenant name")
		}
		td := filepath.Join(basePath, name)
		if err := os.MkdirAll(td, os.FileMode(parsedDirMode)); err != nil {
			log.Fatal().Err(err).Str("path", td).Msg("failed to create tenant directory")
		}
		if err := os.MkdirAll(filepath.Join(td, config.SnapshotsDir), os.FileMode(parsedDirMode)); err != nil {
			log.Fatal().Err(err).Str("path", td).Msg("failed to create tenant snapshots directory")
		}
	}
	log.Info().Int("count", len(tenants)).Msg("tenants configured")

	devices, err := mgr.Devices(ctx, mountPoint)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to resolve block devices")
	}
	for _, d := range devices {
		if d.Missing {
			log.Warn().Str("devid", d.DevID).Str("device", d.Device).Msg("block device missing")
		} else {
			log.Info().Str("devid", d.DevID).Str("device", d.Device).Msg("block device resolved")
		}
	}

	initialStates := make([]DeviceState, len(devices))
	for i, d := range devices {
		initialStates[i] = DeviceState{BTRFSDevice: d}
	}
	s := &Storage{basePath: basePath, mountPoint: mountPoint, quotaEnabled: quotaEnabled, btrfs: mgr, exporter: exporter, tenants: tenants, defaultDirMode: os.FileMode(parsedDirMode), defaultDataMode: dataMode}
	s.cachedDevices.Store(&initialStates)
	return s
}

func (s *Storage) StartWorkers(ctx context.Context, usageInterval, reconcileInterval, deviceIOInterval, deviceStatsInterval time.Duration) {
	for _, tenant := range s.tenants {
		bp := filepath.Join(s.basePath, tenant)
		if s.quotaEnabled {
			StartUsageUpdater(ctx, s.btrfs, bp, usageInterval, tenant)
		}
		if reconcileInterval > 0 {
			s.StartNFSReconciler(ctx, bp, reconcileInterval, tenant)
		}
	}
	s.StartDeviceIOUpdater(ctx, deviceIOInterval)
	s.StartDeviceStatsUpdater(ctx, deviceStatsInterval)
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
