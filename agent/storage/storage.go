package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/meta"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	"github.com/rs/zerolog/log"
)

// Storage encapsulates all btrfs volume, snapshot, and clone operations.
type Storage struct {
	basePath           string
	mountPoint         string
	quotaEnabled       bool
	btrfs              *btrfs.Manager
	exporter           nfs.Exporter
	tenants            []string
	defaultDirMode     os.FileMode
	defaultDataMode    string
	tasks              *task.Manager
	taskDefaultTimeout time.Duration
	taskScrubTimeout   time.Duration
	taskBalanceTimeout time.Duration

	immutableLabelKeys []string

	// maintenanceMu serializes StartScrub and StartBalance so the tasks.List
	// check and subsequent tasks.Create cannot race between concurrent callers.
	// Kept at Storage level so scrub and balance share one lock (they are
	// mutually exclusive by design).
	maintenanceMu sync.Mutex

	volumes   *meta.Store[VolumeMetadata]
	snapshots *meta.Store[SnapshotMetadata]

	// cachedDevices is written by both the IO poller (5s) and btrfs stats poller (1m).
	// Each poller loads the current state, updates its own fields (IO or Errors),
	// and preserves the other poller's fields from the previous snapshot.
	// Uses atomic.Pointer instead of a mutex. Concurrent load+store from two pollers
	// may cause one update to be lost, but the next poll cycle self-corrects
	// (max 5s for IO, max 1m for errors).
	cachedDevices    atomic.Pointer[[]DeviceState]
	cachedFilesystem atomic.Pointer[btrfs.FilesystemUsage]
}

// Config bundles the parameters required to construct a Storage.
type Config struct {
	BasePath        string
	QuotaEnabled    bool
	Exporter        nfs.Exporter
	Tenants         []string
	DefaultDirMode  string
	DefaultDataMode string
	BtrfsBin        string
	ImmutableLabels string

	TaskMaxConcurrent  int
	TaskDefaultTimeout time.Duration
	TaskScrubTimeout   time.Duration
	TaskBalanceTimeout time.Duration
	TaskPollInterval   time.Duration
}

func New(cfg Config) (*Storage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.Exporter == nil {
		return nil, fmt.Errorf("exporter must not be nil")
	}

	parsedDirMode, err := utils.ValidateMode(cfg.DefaultDirMode)
	if err != nil {
		return nil, fmt.Errorf("dir mode: %w", err)
	}
	if _, err := utils.ValidateMode(cfg.DefaultDataMode); err != nil {
		return nil, fmt.Errorf("data mode: %w", err)
	}

	info, err := os.Stat(cfg.BasePath)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("base path %q does not exist or is not a directory", cfg.BasePath)
	}
	if !btrfs.IsBtrfs(cfg.BasePath) {
		return nil, fmt.Errorf("base path %q is not on a btrfs filesystem", cfg.BasePath)
	}
	mountPoint, err := utils.FindMountPoint(cfg.BasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve btrfs mount point for %q: %w", cfg.BasePath, err)
	}
	if mountPoint != cfg.BasePath {
		log.Info().Str("basePath", cfg.BasePath).Str("mountPoint", mountPoint).Msg("base path is a subdirectory of btrfs mount")
	}
	mgr := btrfs.NewManager(cfg.BtrfsBin)
	if !mgr.IsAvailable(ctx) {
		return nil, fmt.Errorf("btrfs tools not found, is btrfs-progs installed?")
	}

	if cfg.QuotaEnabled {
		if err := mgr.QuotaCheck(ctx, cfg.BasePath); err != nil {
			return nil, fmt.Errorf("AGENT_FEATURE_QUOTA_ENABLED=true but btrfs quota is not enabled on %q (run: btrfs quota enable %s): %w", cfg.BasePath, cfg.BasePath, err)
		}
	}

	for _, name := range cfg.Tenants {
		if err := config.ValidateTenantName(name); err != nil {
			return nil, fmt.Errorf("invalid tenant name %q: %w", name, err)
		}
		td := filepath.Join(cfg.BasePath, name)
		if err := os.MkdirAll(td, fileMode(parsedDirMode)); err != nil {
			return nil, fmt.Errorf("create tenant directory %q: %w", td, err)
		}
		if err := os.MkdirAll(filepath.Join(td, config.SnapshotsDir), fileMode(parsedDirMode)); err != nil {
			return nil, fmt.Errorf("create tenant snapshots directory under %q: %w", td, err)
		}
	}
	log.Info().Int("count", len(cfg.Tenants)).Msg("tenants configured")

	devices, err := mgr.Devices(ctx, mountPoint)
	if err != nil {
		return nil, fmt.Errorf("resolve block devices: %w", err)
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
	taskDir := filepath.Join(cfg.BasePath, config.TasksDir)
	tm, err := task.NewManager(taskDir, cfg.TaskMaxConcurrent, cfg.TaskPollInterval)
	if err != nil {
		return nil, fmt.Errorf("init task manager: %w", err)
	}
	s := &Storage{
		basePath:           cfg.BasePath,
		mountPoint:         mountPoint,
		quotaEnabled:       cfg.QuotaEnabled,
		btrfs:              mgr,
		exporter:           cfg.Exporter,
		tenants:            cfg.Tenants,
		defaultDirMode:     fileMode(parsedDirMode),
		defaultDataMode:    cfg.DefaultDataMode,
		immutableLabelKeys: ImmutableLabelKeys(cfg.ImmutableLabels),
		tasks:              tm,
		taskDefaultTimeout: cfg.TaskDefaultTimeout,
		taskScrubTimeout:   cfg.TaskScrubTimeout,
		taskBalanceTimeout: cfg.TaskBalanceTimeout,
		volumes:            meta.NewStore[VolumeMetadata](cfg.BasePath),
		snapshots:          meta.NewStore[SnapshotMetadata](cfg.BasePath, config.SnapshotsDir),
	}
	s.cachedDevices.Store(&initialStates)
	s.loadCache()
	return s, nil
}

func (s *Storage) loadCache() {
	for _, tenant := range s.tenants {
		tenantDir := filepath.Join(s.basePath, tenant)
		volCount, snapCount := 0, 0

		if entries, err := os.ReadDir(tenantDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() || e.Name() == config.SnapshotsDir {
					continue
				}
				dataDir, err := s.volumes.DataPath(tenant, e.Name())
				if err != nil {
					log.Warn().Err(err).Str("tenant", tenant).Str("volume", e.Name()).Msg("cache: invalid volume path, skipping")
					continue
				}
				if _, err := os.Stat(dataDir); os.IsNotExist(err) {
					log.Warn().Str("tenant", tenant).Str("volume", e.Name()).Str("path", dataDir).Msg("cache: data directory missing, skipping phantom volume")
					continue
				}
				if _, err := s.volumes.LoadFromDisk(tenant, e.Name()); err != nil {
					log.Warn().Err(err).Str("tenant", tenant).Str("volume", e.Name()).Msg("cache: corrupt metadata, skipping")
					continue
				}
				log.Debug().Str("tenant", tenant).Str("volume", e.Name()).Msg("cache: loaded volume")
				volCount++
			}
		}

		snapDir := filepath.Join(tenantDir, config.SnapshotsDir)
		if entries, err := os.ReadDir(snapDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				dataDir, err := s.snapshots.DataPath(tenant, e.Name())
				if err != nil {
					log.Warn().Err(err).Str("tenant", tenant).Str("snapshot", e.Name()).Msg("cache: invalid snapshot path, skipping")
					continue
				}
				if _, err := os.Stat(dataDir); os.IsNotExist(err) {
					log.Warn().Str("tenant", tenant).Str("snapshot", e.Name()).Str("path", dataDir).Msg("cache: data directory missing, skipping phantom snapshot")
					continue
				}
				if _, err := s.snapshots.LoadFromDisk(tenant, e.Name()); err != nil {
					log.Warn().Err(err).Str("tenant", tenant).Str("snapshot", e.Name()).Msg("cache: corrupt metadata, skipping")
					continue
				}
				log.Debug().Str("tenant", tenant).Str("snapshot", e.Name()).Msg("cache: loaded snapshot")
				snapCount++
			}
		}

		log.Info().Str("tenant", tenant).Int("volumes", volCount).Int("snapshots", snapCount).Msg("metadata cache loaded")
	}
}

func (s *Storage) StartWorkers(ctx context.Context, usageInterval, reconcileInterval, deviceIOInterval, deviceStatsInterval, taskCleanupInterval time.Duration) {
	for _, tenant := range s.tenants {
		if s.quotaEnabled {
			s.startUsageUpdater(ctx, s.btrfs, usageInterval, tenant)
		}
		if reconcileInterval > 0 {
			s.startNFSReconciler(ctx, reconcileInterval, tenant)
		}
	}
	s.startDeviceIOUpdater(ctx, deviceIOInterval)
	s.startDeviceStatsUpdater(ctx, deviceStatsInterval)
	s.tasks.StartCleanup(ctx, taskCleanupInterval)
}

func (s *Storage) BasePath() string       { return s.basePath }
func (s *Storage) QuotaEnabled() bool     { return s.quotaEnabled }
func (s *Storage) Exporter() nfs.Exporter { return s.exporter }
func (s *Storage) Tasks() *task.Manager   { return s.tasks }

// StartQuotaRescan triggers a filesystem-wide btrfs quota rescan. Mutually
// exclusive with scrub and balance. btrfs has no configurable flags for
// rescan, so opts must be empty.
func (s *Storage) StartQuotaRescan(ctx context.Context, opts map[string]string, labels map[string]string, timeout time.Duration) (string, error) {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if len(opts) > 0 {
		return "", &config.ValidationError{Message: "quota rescan accepts no options"}
	}
	if err := s.ensureMaintenanceFree(ctx); err != nil {
		return "", err
	}
	if err := config.ValidateLabels(labels); err != nil {
		return "", err
	}

	t := s.taskDefaultTimeout
	if timeout > 0 {
		t = timeout
	}
	id := s.tasks.Create(string(task.TypeQuotaRescan), task.TaskOpts{Labels: labels, Timeout: t}, func(ctx context.Context, update *task.Update) error {
		return s.runQuotaRescan(ctx, update)
	})
	log.Info().Str("task", id).Str("path", s.mountPoint).Msg("quota rescan started")
	return id, nil
}

func (s *Storage) runQuotaRescan(ctx context.Context, update *task.Update) error {
	update.SetProgress(50)
	if err := s.btrfs.QuotaRescan(ctx, s.mountPoint); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		// Simple qgroups (squota, kernel 6.7+) do not support rescan; btrfs
		// emits "Invalid argument" for this case. Translate for clarity.
		if strings.Contains(err.Error(), "Invalid argument") {
			return fmt.Errorf("quota rescan not supported on this filesystem (simple quotas / squota mode cannot be rescanned)")
		}
		return fmt.Errorf("btrfs quota rescan: %w", err)
	}
	return nil
}

// ensureMaintenanceFree checks that no scrub, balance, or quota-rescan is
// currently Running/Pending on the agent and that the kernel reports no such
// operation in progress. Callers must hold s.maintenanceMu.
func (s *Storage) ensureMaintenanceFree(ctx context.Context) error {
	for _, t := range s.tasks.List(string(task.TypeScrub), string(task.TypeBalance), string(task.TypeQuotaRescan)) {
		if t.Status == task.TaskRunning || t.Status == task.TaskPending {
			return &StorageError{Code: ErrBusy, Message: "another maintenance task is already running"}
		}
	}
	if sst, err := s.btrfs.ScrubStatus(ctx, s.mountPoint); err == nil && sst.Running {
		return &StorageError{Code: ErrBusy, Message: "scrub already running on filesystem"}
	}
	if bst, _ := s.btrfs.BalanceStatus(ctx, s.mountPoint); bst != nil && bst.Running {
		return &StorageError{Code: ErrBusy, Message: "balance already running on filesystem"}
	}
	if qst, _ := s.btrfs.QuotaRescanStatus(ctx, s.mountPoint); qst != nil && qst.Running {
		return &StorageError{Code: ErrBusy, Message: "quota rescan already running on filesystem"}
	}
	return nil
}

func (s *Storage) tenantPath(tenant string) (string, error) {
	if err := config.ValidateName(tenant); err != nil {
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
