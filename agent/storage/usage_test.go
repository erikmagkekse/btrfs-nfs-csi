package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/meta"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- utils ---

// qgroupRunFn returns a RunFn that responds to btrfs subvolume show + qgroup show.
func qgroupRunFn(referenced, exclusive uint64) func([]string) (string, error) {
	return func(args []string) (string, error) {
		if len(args) >= 1 && args[0] == "subvolume" {
			return "Subvolume ID:\t\t\t256\n", nil
		}
		if len(args) >= 1 && args[0] == "qgroup" {
			return fmt.Sprintf("0/256\t%d\t%d\n", referenced, exclusive), nil
		}
		return "", fmt.Errorf("unexpected command: %v", args)
	}
}

// usageTestStorage creates a Storage with proper base/tenant structure for usage tests.
func usageTestStorage(t *testing.T, tenant string) (*Storage, string) {
	t.Helper()
	base := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				meta.ClearImmutable(path)
			}
			return nil
		})
	})
	tenantPath := filepath.Join(base, tenant)
	require.NoError(t, os.MkdirAll(tenantPath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tenantPath, config.SnapshotsDir), 0o755))
	s := &Storage{
		basePath:  base,
		volumes:   meta.NewStore[VolumeMetadata](base),
		snapshots: meta.NewStore[SnapshotMetadata](base, config.SnapshotsDir),
	}
	return s, tenantPath
}

// setupUsageVol creates a volume dir with data/ subdir (chmod 0755) and metadata.
func setupUsageVol(t *testing.T, s *Storage, tenantPath, tenant, name string, m VolumeMetadata) string {
	t.Helper()
	volDir := seedVolume(t, s, tenant, tenantPath, m)
	dataDir := filepath.Join(volDir, config.DataDir)
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.Chmod(dataDir, 0o755))
	return volDir
}

// setupUsageSnap creates a snapshot dir with data/ subdir and metadata.
func setupUsageSnap(t *testing.T, s *Storage, tenantPath, tenant, name string, m SnapshotMetadata) string {
	t.Helper()
	snapDir := seedSnapshot(t, s, tenant, tenantPath, m)
	dataDir := filepath.Join(snapDir, config.DataDir)
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	return snapDir
}

// readSnapMeta reads SnapshotMetadata from disk.
func readSnapMeta(t *testing.T, snapDir string) SnapshotMetadata {
	t.Helper()
	var meta SnapshotMetadata
	readTestJSON(t, filepath.Join(snapDir, config.MetadataFile), &meta)
	return meta
}

func cleanupMetrics(t *testing.T, tenant string, volumes ...string) {
	t.Helper()
	t.Cleanup(func() {
		VolumesGauge.DeleteLabelValues(tenant)
		for _, vol := range volumes {
			VolumeSizeBytes.DeleteLabelValues(tenant, vol)
			VolumeUsedBytes.DeleteLabelValues(tenant, vol)
		}
	})
}

// --- Tests ---

func TestUpdateAllEmpty(t *testing.T) {
	tenant := "empty"
	runner := &utils.MockRunner{}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, _ := usageTestStorage(t, tenant)
	s.updateAll(context.Background(), mgr, tenant)

	assert.Equal(t, float64(0), testutil.ToFloat64(VolumesGauge.WithLabelValues(tenant)))
	assert.Empty(t, runner.Calls)
	cleanupMetrics(t, tenant)
}

func TestUpdateAllNoChanges(t *testing.T) {
	tenant := "nochange"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{RunFn: qgroupRunFn(1024, 512)}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	initialTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	volDir := setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 4096,
		UsedBytes:  1024,
		UpdatedAt:  initialTime,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta := readVolumeMeta(t, volDir)
	assert.Equal(t, initialTime, meta.UpdatedAt, "UpdatedAt should not change when nothing drifted")
	assert.Equal(t, uid, meta.UID)
	assert.Equal(t, gid, meta.GID)
	assert.Equal(t, "755", meta.Mode)
	assert.Equal(t, uint64(1024), meta.UsedBytes)
	assert.Len(t, runner.Calls, 2, "QgroupUsage should still be called (subvolume show + qgroup show)")
	cleanupMetrics(t, tenant, "vol1")
}

func TestUpdateAllUIDGIDDrift(t *testing.T) {
	tenant := "uiddrift"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	volDir := setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        9999,
		GID:        9999,
		Mode:       "755",
		QuotaBytes: 0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta := readVolumeMeta(t, volDir)
	assert.Equal(t, uid, meta.UID, "UID should be updated to match FS")
	assert.Equal(t, gid, meta.GID, "GID should be updated to match FS")
	assert.False(t, meta.UpdatedAt.IsZero(), "UpdatedAt should be set after drift fix")
	cleanupMetrics(t, tenant, "vol1")
}

func TestUpdateAllModeDrift(t *testing.T) {
	tenant := "modedrift"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	volDir := setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 0,
	})
	require.NoError(t, os.Chmod(filepath.Join(volDir, config.DataDir), 0o700))

	s.updateAll(context.Background(), mgr, tenant)

	meta := readVolumeMeta(t, volDir)
	assert.Equal(t, "700", meta.Mode, "Mode should be updated to match FS")
	cleanupMetrics(t, tenant, "vol1")
}

func TestUpdateAllUsageDrift(t *testing.T) {
	tenant := "usagedrift"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{RunFn: qgroupRunFn(2048, 1024)}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	volDir := setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 4096,
		UsedBytes:  0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta := readVolumeMeta(t, volDir)
	assert.Equal(t, uint64(2048), meta.UsedBytes, "UsedBytes should be updated from qgroup")
	cleanupMetrics(t, tenant, "vol1")
}

func TestUpdateAllNoQuota(t *testing.T) {
	tenant := "noquota"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	assert.Empty(t, runner.Calls, "no qgroup calls when QuotaBytes=0")
	cleanupMetrics(t, tenant, "vol1")
}

func TestUpdateAllQgroupError(t *testing.T) {
	tenant := "qgerr"
	uid, gid := os.Getuid(), os.Getgid()

	runFn := func(args []string) (string, error) {
		path := args[len(args)-1]
		if strings.Contains(path, "/vol1/") {
			return "", fmt.Errorf("qgroup error")
		}
		if args[0] == "subvolume" {
			return "Subvolume ID:\t\t\t256\n", nil
		}
		if args[0] == "qgroup" {
			return "0/256\t2048\t1024\n", nil
		}
		return "", fmt.Errorf("unexpected: %v", args)
	}

	runner := &utils.MockRunner{RunFn: runFn}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 4096,
		UsedBytes:  0,
	})
	volDir2 := setupUsageVol(t, s, tp, tenant, "vol2", VolumeMetadata{
		Name:       "vol2",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 4096,
		UsedBytes:  0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta2 := readVolumeMeta(t, volDir2)
	assert.Equal(t, uint64(2048), meta2.UsedBytes, "vol2 should be updated despite vol1 qgroup error")
	cleanupMetrics(t, tenant, "vol1", "vol2")
}

func TestUpdateAllSnapshots(t *testing.T) {
	tenant := "snaps"
	runner := &utils.MockRunner{RunFn: qgroupRunFn(2048, 512)}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	snapDir := setupUsageSnap(t, s, tp, tenant, "snap1", SnapshotMetadata{
		Name:           "snap1",
		Volume:         "vol1",
		UsedBytes:      0,
		ExclusiveBytes: 0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta := readSnapMeta(t, snapDir)
	assert.Equal(t, uint64(2048), meta.UsedBytes, "UsedBytes should be updated")
	assert.Equal(t, uint64(512), meta.ExclusiveBytes, "ExclusiveBytes should be updated")
	assert.False(t, meta.UpdatedAt.IsZero(), "UpdatedAt should be set")
	cleanupMetrics(t, tenant)
}

func TestUpdateAllSnapshotNoChanges(t *testing.T) {
	tenant := "snapnoch"
	runner := &utils.MockRunner{RunFn: qgroupRunFn(1024, 512)}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	initialTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	snapDir := setupUsageSnap(t, s, tp, tenant, "snap1", SnapshotMetadata{
		Name:           "snap1",
		Volume:         "vol1",
		UsedBytes:      1024,
		ExclusiveBytes: 512,
		UpdatedAt:      initialTime,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta := readSnapMeta(t, snapDir)
	assert.Equal(t, initialTime, meta.UpdatedAt, "UpdatedAt should not change when nothing drifted")
	cleanupMetrics(t, tenant)
}

func TestUpdateAllSnapshotQgroupError(t *testing.T) {
	tenant := "snaperr"
	runner := &utils.MockRunner{Err: fmt.Errorf("qgroup error")}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	snapDir := setupUsageSnap(t, s, tp, tenant, "snap1", SnapshotMetadata{
		Name:           "snap1",
		Volume:         "vol1",
		UsedBytes:      100,
		ExclusiveBytes: 50,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta := readSnapMeta(t, snapDir)
	assert.Equal(t, uint64(100), meta.UsedBytes, "UsedBytes should be unchanged on error")
	assert.Equal(t, uint64(50), meta.ExclusiveBytes, "ExclusiveBytes should be unchanged on error")
	cleanupMetrics(t, tenant)
}

func TestUpdateAllNoSnapshotsDir(t *testing.T) {
	tenant := "nosnapdir"
	runner := &utils.MockRunner{}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	// Create without snapshots dir
	base := t.TempDir()
	tenantPath := filepath.Join(base, tenant)
	require.NoError(t, os.MkdirAll(tenantPath, 0o755))
	s := &Storage{
		basePath:  base,
		volumes:   meta.NewStore[VolumeMetadata](base),
		snapshots: meta.NewStore[SnapshotMetadata](base, config.SnapshotsDir),
	}

	assert.NotPanics(t, func() {
		s.updateAll(context.Background(), mgr, tenant)
	})
	cleanupMetrics(t, tenant)
}

func TestUpdateAllMetrics(t *testing.T) {
	tenant := "metrics"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{RunFn: qgroupRunFn(500, 200)}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)
	setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 4096,
		UsedBytes:  500,
	})
	setupUsageVol(t, s, tp, tenant, "vol2", VolumeMetadata{
		Name:       "vol2",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 8192,
		UsedBytes:  500,
	})

	s.updateAll(context.Background(), mgr, tenant)

	assert.Equal(t, float64(2), testutil.ToFloat64(VolumesGauge.WithLabelValues(tenant)))
	assert.Equal(t, float64(4096), testutil.ToFloat64(VolumeSizeBytes.WithLabelValues(tenant, "vol1")))
	assert.Equal(t, float64(8192), testutil.ToFloat64(VolumeSizeBytes.WithLabelValues(tenant, "vol2")))
	assert.Equal(t, float64(500), testutil.ToFloat64(VolumeUsedBytes.WithLabelValues(tenant, "vol1")))
	assert.Equal(t, float64(500), testutil.ToFloat64(VolumeUsedBytes.WithLabelValues(tenant, "vol2")))
	cleanupMetrics(t, tenant, "vol1", "vol2")
}

func TestUpdateAllStatError(t *testing.T) {
	tenant := "staterr"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{RunFn: qgroupRunFn(2048, 1024)}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)

	// vol1: no data/ dir, os.Stat fails -- but must be in cache
	seedVolume(t, s, tenant, tp, VolumeMetadata{Name: "vol1", QuotaBytes: 4096})

	// vol2: valid, should still be processed
	setupUsageVol(t, s, tp, tenant, "vol2", VolumeMetadata{
		Name:       "vol2",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 4096,
		UsedBytes:  0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	meta2 := readVolumeMeta(t, filepath.Join(tp, "vol2"))
	assert.Equal(t, uint64(2048), meta2.UsedBytes, "vol2 should be updated despite vol1 stat error")
	assert.Equal(t, float64(2), testutil.ToFloat64(VolumesGauge.WithLabelValues(tenant)))
	cleanupMetrics(t, tenant, "vol1", "vol2")
}

func TestUpdateAllSkipsNonDirsAndSnapshots(t *testing.T) {
	tenant := "skiptest"
	uid, gid := os.Getuid(), os.Getgid()
	runner := &utils.MockRunner{}
	mgr := btrfs.NewManagerWithRunner("btrfs", runner)

	s, tp := usageTestStorage(t, tenant)

	// Valid volume (only volumes in cache are iterated now)
	setupUsageVol(t, s, tp, tenant, "vol1", VolumeMetadata{
		Name:       "vol1",
		UID:        uid,
		GID:        gid,
		Mode:       "755",
		QuotaBytes: 0,
	})

	s.updateAll(context.Background(), mgr, tenant)

	assert.Equal(t, float64(1), testutil.ToFloat64(VolumesGauge.WithLabelValues(tenant)))
	cleanupMetrics(t, tenant, "vol1")
}
