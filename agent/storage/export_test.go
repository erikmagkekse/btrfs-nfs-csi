package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- TestCreateVolumeExport ---

func TestCreateVolumeExport(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.CreateVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.NoError(t, err, "CreateVolumeExport")

		meta := readVolumeMeta(t, volDir)
		require.Len(t, meta.Exports, 1)
		assert.Equal(t, "10.0.0.1", meta.Exports[0].IP)
		assert.NotNil(t, meta.LastAttachAt, "LastAttachAt should be set")
		exporter.AssertExpectations(t)
	})

	t.Run("idempotent_client", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{{IP: "10.0.0.1"}},
		})

		// idempotent: same IP+labels already exists, no Export call expected
		err := s.CreateVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.NoError(t, err, "CreateVolumeExport (idempotent)")

		meta := readVolumeMeta(t, volDir)
		count := 0
		for _, c := range meta.Exports {
			if c.IP == "10.0.0.1" {
				count++
			}
		}
		assert.Equal(t, 1, count,
			"expected exactly 1 entry for 10.0.0.1, got %d in: %v", count, meta.Exports)
	})

	t.Run("not_found", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		err := s.CreateVolumeExport(ctx, "test", "nonexistent", "10.0.0.1", nil)
		requireStorageError(t, err, ErrNotFound)
	})

	t.Run("metadata_first_on_export_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("nfs error"))

		err := s.CreateVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nfs export failed")

		// metadata should already have the client (written before export call)
		meta := readVolumeMeta(t, volDir)
		require.Len(t, meta.Exports, 1)
		assert.Equal(t, "10.0.0.1", meta.Exports[0].IP)
		assert.NotNil(t, meta.LastAttachAt)
		exporter.AssertExpectations(t)
	})

	t.Run("ref_counting_same_ip", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{Name: "myvol"})

		// first export for this IP: Export called
		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil).Once()

		labels1 := map[string]string{"kubernetes.volume.id": "vol1"}
		labels2 := map[string]string{"kubernetes.volume.id": "vol2"}

		require.NoError(t, s.CreateVolumeExport(ctx, "test", "myvol", "10.0.0.1", labels1))
		// second export for same IP with different labels: no Export call
		require.NoError(t, s.CreateVolumeExport(ctx, "test", "myvol", "10.0.0.1", labels2))

		meta := readVolumeMeta(t, volDir)
		assert.Len(t, meta.Exports, 2, "should have 2 client refs")
		exporter.AssertExpectations(t)
	})
}

// --- TestDeleteVolumeExport ---

func TestDeleteVolumeExport(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{{IP: "10.0.0.1"}, {IP: "10.0.0.2"}},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.NoError(t, err, "DeleteVolumeExport")

		meta := readVolumeMeta(t, volDir)
		require.Len(t, meta.Exports, 1)
		assert.Equal(t, "10.0.0.2", meta.Exports[0].IP, "other client should remain")
		exporter.AssertExpectations(t)
	})

	t.Run("client_not_in_list", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{{IP: "10.0.0.1"}},
		})

		// IP was never present -> no Unexport call
		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.99", nil)
		require.NoError(t, err, "DeleteVolumeExport (client not in list)")

		meta := readVolumeMeta(t, volDir)
		require.Len(t, meta.Exports, 1)
		assert.Equal(t, "10.0.0.1", meta.Exports[0].IP, "existing client should be preserved")
		exporter.AssertNotCalled(t, "Unexport", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("metadata_first_on_unexport_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{{IP: "10.0.0.1"}},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("nfs error"))

		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nfs unexport failed")

		// metadata should already have client removed (written before unexport call)
		meta := readVolumeMeta(t, volDir)
		assert.Empty(t, meta.Exports,
			"client should be removed from metadata even though unexport failed")
		exporter.AssertExpectations(t)
	})

	t.Run("unexport_with_labels_keeps_other", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		labels1 := map[string]string{"kubernetes.volume.id": "vol1"}
		labels2 := map[string]string{"kubernetes.volume.id": "vol2"}
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{
				{IP: "10.0.0.1", Labels: labels1},
				{IP: "10.0.0.1", Labels: labels2},
			},
		})

		// remove only one ref; IP still has another -> no Unexport call
		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.1", labels1)
		require.NoError(t, err)

		meta := readVolumeMeta(t, volDir)
		require.Len(t, meta.Exports, 1)
		assert.Equal(t, labels2, meta.Exports[0].Labels)
		exporter.AssertExpectations(t) // no Unexport called
	})

	t.Run("unexport_last_entry_triggers_unexport", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		labels1 := map[string]string{"kubernetes.volume.id": "vol1"}
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{
				{IP: "10.0.0.1", Labels: labels1},
			},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.1", labels1)
		require.NoError(t, err)

		meta := readVolumeMeta(t, volDir)
		assert.Empty(t, meta.Exports)
		exporter.AssertExpectations(t) // Unexport called
	})

	t.Run("unexport_nil_labels_removes_all", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{
				{IP: "10.0.0.1", Labels: map[string]string{"kubernetes.volume.id": "vol1"}},
				{IP: "10.0.0.1", Labels: map[string]string{"kubernetes.volume.id": "vol2"}},
			},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(nil)

		// nil labels -> remove all refs for this IP
		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.NoError(t, err)

		meta := readVolumeMeta(t, volDir)
		assert.Empty(t, meta.Exports)
		exporter.AssertExpectations(t)
	})
}

// --- TestListExportsPaginated ---

func TestListExportsPaginated(t *testing.T) {
	t.Run("from_metadata", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		vol1 := filepath.Join(bp, "vol1")
		vol2 := filepath.Join(bp, "vol2")
		require.NoError(t, os.MkdirAll(vol1, 0o755))
		require.NoError(t, os.MkdirAll(vol2, 0o755))
		now := time.Now().UTC()
		writeTestMetadata(t, s, vol1, VolumeMetadata{
			Name: "vol1", Exports: []ExportMetadata{
				{IP: "10.0.0.1", Labels: map[string]string{"created-by": "csi"}, CreatedAt: now},
				{IP: "10.0.0.2", CreatedAt: now.Add(-time.Minute)},
			},
		})
		writeTestMetadata(t, s, vol2, VolumeMetadata{
			Name: "vol2", Exports: []ExportMetadata{{IP: "10.0.0.3", CreatedAt: now.Add(-2 * time.Minute)}},
		})

		page, err := s.ListVolumeExportsPaginated("test", "", 0)
		require.NoError(t, err)
		assert.Equal(t, 3, page.Total)
		require.Len(t, page.Items, 3)
		// sorted newest first
		assert.Equal(t, "10.0.0.1", page.Items[0].Client)
		assert.Equal(t, "csi", page.Items[0].Labels["created-by"])
		assert.Equal(t, "10.0.0.3", page.Items[2].Client)
	})

	t.Run("empty", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		page, err := s.ListVolumeExportsPaginated("test", "", 0)
		require.NoError(t, err)
		assert.Empty(t, page.Items)
	})
}
