package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// testStorageWithExporter creates a Storage with a temp dir and mock exporter.
// Returns the Storage and the tenant base path (basePath/tenant).
func testStorageWithExporter(t *testing.T, exporter *nfs.MockExporter) (*Storage, string) {
	t.Helper()
	base := t.TempDir()
	tenant := "test"
	tenantPath := filepath.Join(base, tenant)
	require.NoError(t, os.MkdirAll(tenantPath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tenantPath, config.SnapshotsDir), 0o755))

	mgr := btrfs.NewManagerWithRunner("btrfs", &utils.MockRunner{})
	s := &Storage{
		basePath:       base,
		btrfs:          mgr,
		exporter:       exporter,
		tenants:        []string{tenant},
		defaultDirMode: 0o755,
	}
	return s, tenantPath
}

// writeTestMetadata writes a VolumeMetadata JSON into volDir/metadata.json.
func writeTestMetadata(t *testing.T, volDir string, meta VolumeMetadata) {
	t.Helper()
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(volDir, config.MetadataFile), data, 0o644))
}

func TestReconcileExports(t *testing.T) {
	ctx := context.Background()

	t.Run("noop_all_in_sync", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		// Multiple volumes, multiple clients each, all matching
		vol1 := filepath.Join(bp, "vol1")
		vol2 := filepath.Join(bp, "vol2")
		require.NoError(t, os.MkdirAll(vol1, 0o755))
		require.NoError(t, os.MkdirAll(vol2, 0o755))
		writeTestMetadata(t, vol1, VolumeMetadata{
			Name:    "vol1",
			Clients: []string{"10.0.0.1", "10.0.0.2"},
		})
		writeTestMetadata(t, vol2, VolumeMetadata{
			Name:    "vol2",
			Clients: []string{"10.0.0.3"},
		})

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: vol1, Client: "10.0.0.1"},
			{Path: vol1, Client: "10.0.0.2"},
			{Path: vol2, Client: "10.0.0.3"},
			{Path: "/other/tenant/vol", Client: "10.0.0.99"}, // outside basepath
		}, nil)

		s.reconcileExports(ctx, bp, "test")

		exporter.AssertExpectations(t)
		exporter.AssertNotCalled(t, "Export", mock.Anything, mock.Anything, mock.Anything)
		exporter.AssertNotCalled(t, "Unexport", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("orphan_removed", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		// Healthy volume that should not be touched
		healthy := filepath.Join(bp, "healthy")
		require.NoError(t, os.MkdirAll(healthy, 0o755))
		writeTestMetadata(t, healthy, VolumeMetadata{
			Name:    "healthy",
			Clients: []string{"10.0.0.1"},
		})

		// Orphan: dir was deleted but export still exists
		deletedPath := filepath.Join(bp, "deleted-vol")

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: healthy, Client: "10.0.0.1"},
			{Path: deletedPath, Client: "10.0.0.5"},
			{Path: deletedPath, Client: "10.0.0.6"}, // second client for same orphan
		}, nil)
		exporter.On("Unexport", mock.Anything, deletedPath, "").Return(nil)

		s.reconcileExports(ctx, bp, "test")

		exporter.AssertExpectations(t)
		// Orphan unexported, healthy volume untouched
		exporter.AssertCalled(t, "Unexport", mock.Anything, deletedPath, "")
		exporter.AssertNotCalled(t, "Export", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("missing_export_restored", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		// vol1: has 2 clients in metadata, but only 1 is exported
		vol1 := filepath.Join(bp, "vol1")
		require.NoError(t, os.MkdirAll(vol1, 0o755))
		writeTestMetadata(t, vol1, VolumeMetadata{
			Name:    "vol1",
			Clients: []string{"10.0.0.1", "10.0.0.2"},
		})

		// vol2: fully missing from exports
		vol2 := filepath.Join(bp, "vol2")
		require.NoError(t, os.MkdirAll(vol2, 0o755))
		writeTestMetadata(t, vol2, VolumeMetadata{
			Name:    "vol2",
			Clients: []string{"10.0.0.3"},
		})

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: vol1, Client: "10.0.0.1"}, // only one of two
		}, nil)
		exporter.On("Export", mock.Anything, vol1, "10.0.0.2").Return(nil)
		exporter.On("Export", mock.Anything, vol2, "10.0.0.3").Return(nil)

		s.reconcileExports(ctx, bp, "test")

		exporter.AssertExpectations(t)
		// 10.0.0.1 already exported, no call; 10.0.0.2 + 10.0.0.3 restored
		exporter.AssertNumberOfCalls(t, "Export", 2)
		exporter.AssertNotCalled(t, "Unexport", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("orphan_removal_failure_continues", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		// Healthy volume with a missing client, should be restored
		healthy := filepath.Join(bp, "healthy")
		require.NoError(t, os.MkdirAll(healthy, 0o755))
		writeTestMetadata(t, healthy, VolumeMetadata{
			Name:    "healthy",
			Clients: []string{"10.0.0.10"},
		})

		orphan1 := filepath.Join(bp, "orphan1")
		orphan2 := filepath.Join(bp, "orphan2")

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: orphan1, Client: "10.0.0.1"},
			{Path: orphan2, Client: "10.0.0.2"},
		}, nil)
		exporter.On("Unexport", mock.Anything, orphan1, "").Return(fmt.Errorf("nfs error"))
		exporter.On("Unexport", mock.Anything, orphan2, "").Return(nil)
		exporter.On("Export", mock.Anything, healthy, "10.0.0.10").Return(nil)

		s.reconcileExports(ctx, bp, "test")

		exporter.AssertExpectations(t)
		// Both orphans attempted despite first failure
		exporter.AssertNumberOfCalls(t, "Unexport", 2)
		// Healthy volume still gets its missing export restored
		exporter.AssertCalled(t, "Export", mock.Anything, healthy, "10.0.0.10")
	})

	t.Run("corrupt_metadata_skipped", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		// Corrupt metadata, should be silently skipped
		corrupt := filepath.Join(bp, "corrupt-vol")
		require.NoError(t, os.MkdirAll(corrupt, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(corrupt, config.MetadataFile),
			[]byte("{not valid json!!!"),
			0o644,
		))

		// Volume dir without any metadata file, also skipped
		noMeta := filepath.Join(bp, "no-meta")
		require.NoError(t, os.MkdirAll(noMeta, 0o755))

		// Healthy volume next to the broken ones, should still be restored
		healthy := filepath.Join(bp, "healthy")
		require.NoError(t, os.MkdirAll(healthy, 0o755))
		writeTestMetadata(t, healthy, VolumeMetadata{
			Name:    "healthy",
			Clients: []string{"10.0.0.1"},
		})

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{}, nil)
		exporter.On("Export", mock.Anything, healthy, "10.0.0.1").Return(nil)

		s.reconcileExports(ctx, bp, "test")

		exporter.AssertExpectations(t)
		// Only healthy volume gets an Export call, broken ones are skipped
		exporter.AssertNumberOfCalls(t, "Export", 1)
		exporter.AssertCalled(t, "Export", mock.Anything, healthy, "10.0.0.1")
	})

	t.Run("exports_outside_basepath_ignored", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		// Healthy volume in basepath
		vol1 := filepath.Join(bp, "vol1")
		require.NoError(t, os.MkdirAll(vol1, 0o755))
		writeTestMetadata(t, vol1, VolumeMetadata{
			Name:    "vol1",
			Clients: []string{"10.0.0.1"},
		})

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: "/other/path/vol", Client: "10.0.0.99"},        // different tenant
			{Path: "/mnt/other-cluster/data", Client: "10.0.0.5"}, // completely unrelated
			{Path: vol1, Client: "10.0.0.1"},                      // in sync
		}, nil)

		s.reconcileExports(ctx, bp, "test")

		exporter.AssertExpectations(t)
		// Outside exports ignored, in-sync volume not touched
		exporter.AssertNotCalled(t, "Unexport", mock.Anything, mock.Anything, mock.Anything)
		exporter.AssertNotCalled(t, "Export", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("list_exports_error_aborts", func(t *testing.T) {
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithExporter(t, exporter)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo(nil), fmt.Errorf("rpc error"))

		assert.NotPanics(t, func() {
			s.reconcileExports(ctx, bp, "test")
		})

		exporter.AssertExpectations(t)
		exporter.AssertNotCalled(t, "Export", mock.Anything, mock.Anything, mock.Anything)
		exporter.AssertNotCalled(t, "Unexport", mock.Anything, mock.Anything, mock.Anything)
	})
}
