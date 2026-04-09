package driver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/mount-utils"
)

// --- NodeGetVolumeStats tests ---

func TestNodeGetVolumeStats_MissingVolumePath(t *testing.T) {
	ns := newTestNodeServer(nil)
	_, err := ns.NodeGetVolumeStats(context.Background(), &csi.NodeGetVolumeStatsRequest{
		VolumeId: "sc|vol-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestNodeGetVolumeStats_QuotaUsage(t *testing.T) {
	tmp := t.TempDir()
	volumePath := filepath.Join(tmp, "publish")
	stagingPath := filepath.Join(tmp, "staging")
	require.NoError(t, os.MkdirAll(volumePath, 0o755))
	require.NoError(t, os.MkdirAll(stagingPath, 0o755))

	data, err := json.Marshal(volumeStats{QuotaBytes: 1 << 30, UsedBytes: 100 << 20})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stagingPath, config.MetadataFile), data, 0o644))

	ns := newTestNodeServer(nil)

	resp, err := ns.NodeGetVolumeStats(context.Background(), &csi.NodeGetVolumeStatsRequest{
		VolumeId:          "sc|vol-quota",
		VolumePath:        volumePath,
		StagingTargetPath: stagingPath,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Usage, 1)
	assert.Equal(t, int64(1<<30), resp.Usage[0].Total)
	assert.Equal(t, int64(100<<20), resp.Usage[0].Used)
	assert.Equal(t, int64((1<<30)-(100<<20)), resp.Usage[0].Available)
}

func TestNodeGetVolumeStats_StatfsFallback(t *testing.T) {
	tmp := t.TempDir()
	volumePath := filepath.Join(tmp, "publish")
	stagingPath := filepath.Join(tmp, "staging")
	require.NoError(t, os.MkdirAll(volumePath, 0o755))
	require.NoError(t, os.MkdirAll(stagingPath, 0o755))

	// quota_bytes=0 -> triggers statfs fallback.
	data, err := json.Marshal(volumeStats{QuotaBytes: 0, UsedBytes: 0})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stagingPath, config.MetadataFile), data, 0o644))

	ns := newTestNodeServer(nil)

	resp, err := ns.NodeGetVolumeStats(context.Background(), &csi.NodeGetVolumeStatsRequest{
		VolumeId:          "sc|vol-fallback",
		VolumePath:        volumePath,
		StagingTargetPath: stagingPath,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Usage, 1)
	assert.Greater(t, resp.Usage[0].Total, int64(0), "statfs should yield non-zero total")
}

func TestNodeGetVolumeStats_MetadataMissing(t *testing.T) {
	tmp := t.TempDir()
	volumePath := filepath.Join(tmp, "publish")
	stagingPath := filepath.Join(tmp, "staging")
	require.NoError(t, os.MkdirAll(volumePath, 0o755))
	require.NoError(t, os.MkdirAll(stagingPath, 0o755))

	// No metadata.json written -> ReadFile fails, final error branch.
	ns := newTestNodeServer(nil)

	_, err := ns.NodeGetVolumeStats(context.Background(), &csi.NodeGetVolumeStatsRequest{
		VolumeId:          "sc|vol-missing",
		VolumePath:        volumePath,
		StagingTargetPath: stagingPath,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- findStagingPath tests (migrated from health_test.go) ---

func TestFindStagingPath(t *testing.T) {
	ns := newTestNodeServer([]mount.MountPoint{
		{Device: "10.0.0.5:/exports/tenant/vol-abc", Path: "/var/lib/kubelet/plugins/csi/pv/pv-1/globalmount", Type: "nfs4"},
		{Device: "10.0.0.5:/exports/tenant/vol-xyz", Path: "/var/lib/kubelet/plugins/csi/pv/pv-2/globalmount", Type: "nfs4"},
		{Device: "/dev/sda1", Path: "/", Type: "ext4"},
	})

	t.Run("found", func(t *testing.T) {
		path := ns.findStagingPath("sc|vol-abc")
		assert.Equal(t, "/var/lib/kubelet/plugins/csi/pv/pv-1/globalmount", path)
	})

	t.Run("not_found", func(t *testing.T) {
		path := ns.findStagingPath("sc|vol-missing")
		assert.Empty(t, path)
	})

	t.Run("invalid_volume_id", func(t *testing.T) {
		path := ns.findStagingPath("invalid")
		assert.Empty(t, path)
	})
}

// --- test helpers ---

func newTestNodeServer(mps []mount.MountPoint) *NodeServer {
	return &NodeServer{
		nodeID:  "test-node",
		nodeIP:  "10.0.0.1",
		mounter: mount.NewFakeMounter(mps),
	}
}
