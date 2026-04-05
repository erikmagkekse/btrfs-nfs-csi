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

const testLabelVolumeID = "kubernetes.volume.id"

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

	t.Run("invalid_client_ip", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{Name: "myvol"})

		cases := []struct {
			name   string
			client string
		}{
			{"wildcard", "*"},
			{"hostname", "node1.example.com"},
			{"cidr", "10.0.0.0/24"},
			{"parens_injection", "10.0.0.1(rw,no_root_squash)"},
			{"empty", ""},
			{"space", "10.0.0.1 10.0.0.2"},
			{"newline", "10.0.0.1\n10.0.0.2"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := s.CreateVolumeExport(ctx, "test", "myvol", tc.client, nil)
				requireStorageError(t, err, ErrInvalid)
			})
		}
	})

	t.Run("valid_ipv6_client", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "::1").Return(nil)

		err := s.CreateVolumeExport(ctx, "test", "myvol", "::1", nil)
		require.NoError(t, err, "CreateVolumeExport with IPv6")
		exporter.AssertExpectations(t)
	})

	t.Run("metadata_first_on_export_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("exportfs: /data/vol1: command failed"))

		err := s.CreateVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.Error(t, err)
		requireStorageError(t, err, ErrInternal)
		assert.Equal(t, "nfs export failed", err.Error(), "error should not leak command details")
		assert.NotContains(t, err.Error(), "exportfs", "command name must not leak")

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

		labels1 := map[string]string{testLabelVolumeID: "vol1"}
		labels2 := map[string]string{testLabelVolumeID: "vol2"}

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

	t.Run("invalid_client_ip", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, s, volDir, VolumeMetadata{
			Name: "myvol", Exports: []ExportMetadata{{IP: "10.0.0.1"}},
		})

		err := s.DeleteVolumeExport(ctx, "test", "myvol", "*", nil)
		requireStorageError(t, err, ErrInvalid)

		err = s.DeleteVolumeExport(ctx, "test", "myvol", "node1.example.com", nil)
		requireStorageError(t, err, ErrInvalid)
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

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("exportfs: /data/vol1: command failed"))

		err := s.DeleteVolumeExport(ctx, "test", "myvol", "10.0.0.1", nil)
		require.Error(t, err)
		requireStorageError(t, err, ErrInternal)
		assert.Equal(t, "nfs unexport failed", err.Error(), "error should not leak command details")
		assert.NotContains(t, err.Error(), "exportfs", "command name must not leak")

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
		labels1 := map[string]string{testLabelVolumeID: "vol1"}
		labels2 := map[string]string{testLabelVolumeID: "vol2"}
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
		labels1 := map[string]string{testLabelVolumeID: "vol1"}
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
				{IP: "10.0.0.1", Labels: map[string]string{testLabelVolumeID: "vol1"}},
				{IP: "10.0.0.1", Labels: map[string]string{testLabelVolumeID: "vol2"}},
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

// --- TestListVolumeExports ---

func TestListVolumeExports(t *testing.T) {
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

		entries, err := s.ListVolumeExports("test")
		require.NoError(t, err)
		require.Len(t, entries, 3)
		// unsorted from storage; handler applies sort order
		clients := map[string]bool{}
		for _, e := range entries {
			clients[e.Client] = true
		}
		assert.True(t, clients["10.0.0.1"])
		assert.True(t, clients["10.0.0.2"])
		assert.True(t, clients["10.0.0.3"])
		// verify labels are preserved
		for _, e := range entries {
			if e.Client == "10.0.0.1" {
				assert.Equal(t, "csi", e.Labels["created-by"])
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		entries, err := s.ListVolumeExports("test")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}
