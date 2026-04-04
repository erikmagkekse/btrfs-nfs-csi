//go:build integration

package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/meta"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"

	"github.com/stretchr/testify/suite"
)

type UtilsIntegrationSuite struct {
	suite.Suite
	mnt     string
	storage *Storage
	ctx     context.Context
}

func TestUtilsIntegration(t *testing.T) {
	suite.Run(t, new(UtilsIntegrationSuite))
}

func (s *UtilsIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	mnt, err := utils.FindMountPoint("/")
	s.Require().NoError(err)
	s.mnt = filepath.Join(s.T().TempDir(), "integration")
	s.Require().NoError(os.MkdirAll(s.mnt, 0o755))

	tenant := "test"
	tenantPath := filepath.Join(s.mnt, tenant)
	s.Require().NoError(os.MkdirAll(tenantPath, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(tenantPath, config.SnapshotsDir), 0o755))

	_ = mnt
	mgr := btrfs.NewManager("btrfs")
	exporter := &nfs.MockExporter{}
	taskDir := filepath.Join(s.mnt, config.TasksDir)
	s.storage = &Storage{
		basePath:        s.mnt,
		mountPoint:      s.mnt,
		btrfs:           mgr,
		exporter:        exporter,
		tenants:         []string{tenant},
		defaultDirMode:  0o755,
		defaultDataMode: "2770",
		tasks:           nil,
		volumes:         meta.NewStore[VolumeMetadata](s.mnt),
		snapshots:       meta.NewStore[SnapshotMetadata](s.mnt, config.SnapshotsDir),
	}
	_ = taskDir
}

func (s *UtilsIntegrationSuite) TestStoreWriteAndRead() {
	store := meta.NewStore[VolumeMetadata](s.mnt)

	volDir := filepath.Join(s.mnt, "test", "testvol")
	s.Require().NoError(os.MkdirAll(volDir, 0o755))

	now := time.Now().Truncate(time.Second).UTC()
	vol := &VolumeMetadata{
		Name:      "testvol",
		Path:      "/data/testvol",
		SizeBytes: 1024,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.Require().NoError(store.Store("test", "testvol", vol))

	got, err := store.Get("test", "testvol")
	s.Require().NoError(err)
	s.Assert().Equal("testvol", got.Name)
	s.Assert().Equal(uint64(1024), got.SizeBytes)

	// verify no .tmp remains
	_, err = os.Stat(store.MetaPath("test", "testvol") + ".tmp")
	s.Assert().True(os.IsNotExist(err))
}

func (s *UtilsIntegrationSuite) TestStoreUpdate() {
	store := meta.NewStore[VolumeMetadata](s.mnt)

	volDir := filepath.Join(s.mnt, "test", "vol-update")
	s.Require().NoError(os.MkdirAll(volDir, 0o755))

	s.Require().NoError(store.Store("test", "vol-update", &VolumeMetadata{Name: "vol-update", SizeBytes: 1024}))

	updated, err := store.Update("test", "vol-update", func(m *VolumeMetadata) {
		m.SizeBytes = 2048
	})
	s.Require().NoError(err)
	s.Assert().Equal(uint64(2048), updated.SizeBytes)
}
