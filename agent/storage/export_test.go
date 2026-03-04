package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- TestExportVolume ---

func TestExportVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.ExportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.NoError(t, err, "ExportVolume")

		meta := readVolumeMeta(t, volDir)
		assert.Contains(t, meta.Clients, "10.0.0.1", "client should be in metadata")
		assert.NotNil(t, meta.LastAttachAt, "LastAttachAt should be set")
		exporter.AssertExpectations(t)
	})

	t.Run("idempotent_client", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1"},
		})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.ExportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.NoError(t, err, "ExportVolume (idempotent)")

		meta := readVolumeMeta(t, volDir)
		count := 0
		for _, c := range meta.Clients {
			if c == "10.0.0.1" {
				count++
			}
		}
		assert.Equal(t, 1, count,
			"expected exactly 1 entry for 10.0.0.1, got %d in: %v", count, meta.Clients)
	})

	t.Run("not_found", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		err := s.ExportVolume(ctx, "test", "nonexistent", "10.0.0.1")
		requireStorageError(t, err, ErrNotFound)
	})

	t.Run("metadata_first_on_export_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("nfs error"))

		err := s.ExportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nfs export failed")

		// metadata should already have the client (written before export call)
		meta := readVolumeMeta(t, volDir)
		assert.Contains(t, meta.Clients, "10.0.0.1",
			"client should be persisted in metadata even though export failed")
		assert.NotNil(t, meta.LastAttachAt)
		exporter.AssertExpectations(t)
	})
}

// --- TestUnexportVolume ---

func TestUnexportVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1", "10.0.0.2"},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.UnexportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.NoError(t, err, "UnexportVolume")

		meta := readVolumeMeta(t, volDir)
		assert.NotContains(t, meta.Clients, "10.0.0.1", "removed client should be gone")
		assert.Contains(t, meta.Clients, "10.0.0.2", "other client should remain")
		exporter.AssertExpectations(t)
	})

	t.Run("client_not_in_list", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1"},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.99").Return(nil)

		err := s.UnexportVolume(ctx, "test", "myvol", "10.0.0.99")
		require.NoError(t, err, "UnexportVolume (client not in list)")

		meta := readVolumeMeta(t, volDir)
		assert.Contains(t, meta.Clients, "10.0.0.1", "existing client should be preserved")
	})

	t.Run("metadata_first_on_unexport_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1"},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("nfs error"))

		err := s.UnexportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nfs unexport failed")

		// metadata should already have client removed (written before unexport call)
		meta := readVolumeMeta(t, volDir)
		assert.NotContains(t, meta.Clients, "10.0.0.1",
			"client should be removed from metadata even though unexport failed")
		exporter.AssertExpectations(t)
	})
}

// --- TestListExports ---

func TestListExports(t *testing.T) {
	ctx := context.Background()

	t.Run("filter_by_tenant", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: filepath.Join(bp, "vol1"), Client: "10.0.0.1"},
			{Path: filepath.Join(bp, "vol2"), Client: "10.0.0.2"},
			{Path: "/other/tenant/vol", Client: "10.0.0.99"},
		}, nil)

		entries, err := s.ListExports(ctx, "test")
		require.NoError(t, err, "ListExports")
		assert.Len(t, entries, 2, "should only include exports under tenant path")
		exporter.AssertExpectations(t)
	})

	t.Run("empty", func(t *testing.T) {
		s, _, _, exporter := newTestStorage(t)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{}, nil)

		entries, err := s.ListExports(ctx, "test")
		require.NoError(t, err, "ListExports")
		assert.Empty(t, entries)
	})

	t.Run("error", func(t *testing.T) {
		s, _, _, exporter := newTestStorage(t)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo(nil), fmt.Errorf("rpc error"))

		_, err := s.ListExports(ctx, "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "list exports failed")
	})
}
