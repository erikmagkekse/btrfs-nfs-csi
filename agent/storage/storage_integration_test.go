//go:build integration

package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type StorageIntegrationSuite struct {
	suite.Suite
	mnt       string
	cmd       *utils.ShellRunner
	ctx       context.Context
	loopDev   string
	storage   *Storage
	tenantDir string
}

func TestStorageIntegration(t *testing.T) {
	suite.Run(t, new(StorageIntegrationSuite))
}

func (s *StorageIntegrationSuite) SetupSuite() {
	t := s.T()

	s.ctx = context.Background()
	s.cmd = &utils.ShellRunner{}

	tmpDir := t.TempDir()
	imgFile := filepath.Join(tmpDir, "btrfs.img")
	s.mnt = filepath.Join(tmpDir, "mnt")

	err := os.MkdirAll(s.mnt, 0o755)
	s.Require().NoError(err, "mkdir")

	_, err = s.cmd.Run(s.ctx, "fallocate", "-l", "1G", imgFile)
	s.Require().NoError(err, "fallocate")

	loopOut, err := s.cmd.Run(s.ctx, "losetup", "--find", "--show", imgFile)
	s.Require().NoError(err, "losetup")
	s.loopDev = strings.TrimSpace(loopOut)

	_, err = s.cmd.Run(s.ctx, "mkfs.btrfs", "-f", s.loopDev)
	if err != nil {
		_, _ = s.cmd.Run(s.ctx, "losetup", "-d", s.loopDev)
		s.Require().NoError(err, "mkfs.btrfs")
	}

	_, err = s.cmd.Run(s.ctx, "mount", s.loopDev, s.mnt)
	if err != nil {
		_, _ = s.cmd.Run(s.ctx, "losetup", "-d", s.loopDev)
		s.Require().NoError(err, "mount")
	}

	_, err = s.cmd.Run(s.ctx, "btrfs", "quota", "enable", s.mnt)
	if err != nil {
		_, _ = s.cmd.Run(s.ctx, "umount", s.mnt)
		_, _ = s.cmd.Run(s.ctx, "losetup", "-d", s.loopDev)
		s.Require().NoError(err, "quota enable")
	}

	// Create tenant directory inside btrfs mount
	s.tenantDir = filepath.Join(s.mnt, "test")
	s.Require().NoError(os.MkdirAll(s.tenantDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.tenantDir, config.SnapshotsDir), 0o755))

	// Default permissive mock exporter
	exporter := &nfs.MockExporter{}
	exporter.On("Export", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	exporter.On("Unexport", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{}, nil).Maybe()

	taskDir := filepath.Join(s.mnt, config.TasksDir)
	s.Require().NoError(os.MkdirAll(taskDir, 0o755))

	s.storage = &Storage{
		basePath:        s.mnt,
		mountPoint:      s.mnt,
		quotaEnabled:    true,
		btrfs:           btrfs.NewManager("btrfs"),
		exporter:        exporter,
		tenants:         []string{"test"},
		defaultDirMode:  0o755,
		defaultDataMode: "2770",
		tasks:           task.NewManager(taskDir, 0),
	}
}

func (s *StorageIntegrationSuite) TearDownSuite() {
	_, _ = s.cmd.Run(s.ctx, "umount", s.mnt)
	_, _ = s.cmd.Run(s.ctx, "losetup", "-d", s.loopDev)
}

func (s *StorageIntegrationSuite) TestVolumeLifecycle() {
	// Create
	meta, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "lifecycle-vol", SizeBytes: 10 * 1024 * 1024,
	})
	s.Require().NoError(err)
	s.Assert().Equal("lifecycle-vol", meta.Name)
	s.Assert().Equal(uint64(10*1024*1024), meta.SizeBytes)

	// Verify real subvolume exists
	dataDir := filepath.Join(s.tenantDir, "lifecycle-vol", config.DataDir)
	s.Assert().True(s.storage.btrfs.SubvolumeExists(s.ctx, dataDir))

	// Get
	got, err := s.storage.GetVolume("test", "lifecycle-vol")
	s.Require().NoError(err)
	s.Assert().Equal(meta.Name, got.Name)
	s.Assert().Equal(meta.SizeBytes, got.SizeBytes)

	// List
	vols, err := s.storage.ListVolumes("test")
	s.Require().NoError(err)
	found := false
	for _, v := range vols {
		if v.Name == "lifecycle-vol" {
			found = true
			break
		}
	}
	s.Assert().True(found, "volume should appear in list")

	// Update
	updated, err := s.storage.UpdateVolume(s.ctx, "test", "lifecycle-vol", VolumeUpdateRequest{
		SizeBytes:   ptrUint64(20 * 1024 * 1024),
		Compression: ptrString("zstd"),
	})
	s.Require().NoError(err)
	s.Assert().Equal(uint64(20*1024*1024), updated.SizeBytes)
	s.Assert().Equal("zstd", updated.Compression)

	// Delete
	err = s.storage.DeleteVolume(s.ctx, "test", "lifecycle-vol")
	s.Require().NoError(err)

	s.Assert().False(s.storage.btrfs.SubvolumeExists(s.ctx, dataDir))
	_, statErr := os.Stat(filepath.Join(s.tenantDir, "lifecycle-vol"))
	s.Assert().True(os.IsNotExist(statErr))
}

func (s *StorageIntegrationSuite) TestVolumeWithQuota() {
	meta, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "quota-vol", SizeBytes: 50 * 1024 * 1024, QuotaBytes: 50 * 1024 * 1024,
	})
	s.Require().NoError(err)
	s.Assert().Equal(uint64(50*1024*1024), meta.QuotaBytes)

	// Write some data
	dataDir := filepath.Join(s.tenantDir, "quota-vol", config.DataDir)
	testData := make([]byte, 64*1024)
	s.Require().NoError(os.WriteFile(filepath.Join(dataDir, "testfile"), testData, 0o644))
	_, err = s.cmd.Run(s.ctx, "sync")
	s.Require().NoError(err)

	// Verify usage
	used, err := s.storage.btrfs.QgroupUsage(s.ctx, dataDir)
	s.Require().NoError(err)
	s.Assert().NotZero(used, "usage should be > 0 after writing data")

	// Update quota (increase)
	updated, err := s.storage.UpdateVolume(s.ctx, "test", "quota-vol", VolumeUpdateRequest{
		SizeBytes: ptrUint64(100 * 1024 * 1024),
	})
	s.Require().NoError(err)
	s.Assert().Equal(uint64(100*1024*1024), updated.SizeBytes)
	s.Assert().Equal(uint64(100*1024*1024), updated.QuotaBytes)
}

func (s *StorageIntegrationSuite) TestVolumePermissions() {
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "perm-vol", SizeBytes: 10 * 1024 * 1024, Mode: "2770",
	})
	s.Require().NoError(err)

	dataDir := filepath.Join(s.tenantDir, "perm-vol", config.DataDir)
	info, err := os.Stat(dataDir)
	s.Require().NoError(err)
	mode := info.Mode()
	s.Assert().True(mode&os.ModeSetgid != 0, "setgid should be set")
	s.Assert().Equal(os.FileMode(0o770), mode.Perm())

	// Update mode
	_, err = s.storage.UpdateVolume(s.ctx, "test", "perm-vol", VolumeUpdateRequest{
		Mode: ptrString("0750"),
	})
	s.Require().NoError(err)

	info, err = os.Stat(dataDir)
	s.Require().NoError(err)
	s.Assert().Equal(os.FileMode(0o750), info.Mode().Perm())
}

func (s *StorageIntegrationSuite) TestVolumeNoCOW() {
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "nocow-vol", SizeBytes: 10 * 1024 * 1024, NoCOW: true,
	})
	s.Require().NoError(err)

	dataDir := filepath.Join(s.tenantDir, "nocow-vol", config.DataDir)
	out, err := s.cmd.Run(s.ctx, "lsattr", "-d", dataDir)
	s.Require().NoError(err)
	s.Assert().Contains(out, "C", "NoCOW attribute should be set")
}

func (s *StorageIntegrationSuite) TestVolumeCompression() {
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "comp-vol", SizeBytes: 10 * 1024 * 1024, Compression: "zstd",
	})
	s.Require().NoError(err)

	dataDir := filepath.Join(s.tenantDir, "comp-vol", config.DataDir)
	out, err := s.cmd.Run(s.ctx, "btrfs", "property", "get", dataDir, "compression")
	s.Require().NoError(err)
	s.Assert().Contains(out, "zstd")
}

func (s *StorageIntegrationSuite) TestSnapshotLifecycle() {
	// Create source volume
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "snap-src", SizeBytes: 10 * 1024 * 1024,
	})
	s.Require().NoError(err)

	// Create snapshot
	snap, err := s.storage.CreateSnapshot(s.ctx, "test", SnapshotCreateRequest{
		Name: "my-snapshot", Volume: "snap-src",
	})
	s.Require().NoError(err)
	s.Assert().Equal("my-snapshot", snap.Name)
	s.Assert().Equal("snap-src", snap.Volume)
	s.Assert().True(snap.ReadOnly)

	// Verify snapshot subvolume is readonly
	snapDataDir := filepath.Join(s.tenantDir, config.SnapshotsDir, "my-snapshot", config.DataDir)
	s.Assert().True(s.storage.btrfs.SubvolumeExists(s.ctx, snapDataDir))

	// Get
	got, err := s.storage.GetSnapshot("test", "my-snapshot")
	s.Require().NoError(err)
	s.Assert().Equal("my-snapshot", got.Name)

	// List (with volume filter)
	snaps, err := s.storage.ListSnapshots("test", "snap-src")
	s.Require().NoError(err)
	s.Assert().Len(snaps, 1)
	s.Assert().Equal("my-snapshot", snaps[0].Name)

	// List (without filter should also include it)
	allSnaps, err := s.storage.ListSnapshots("test", "")
	s.Require().NoError(err)
	found := false
	for _, sn := range allSnaps {
		if sn.Name == "my-snapshot" {
			found = true
			break
		}
	}
	s.Assert().True(found)

	// Delete
	err = s.storage.DeleteSnapshot(s.ctx, "test", "my-snapshot")
	s.Require().NoError(err)
	s.Assert().False(s.storage.btrfs.SubvolumeExists(s.ctx, snapDataDir))
}

func (s *StorageIntegrationSuite) TestCloneLifecycle() {
	// Create source volume and write data
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "clone-src", SizeBytes: 10 * 1024 * 1024,
	})
	s.Require().NoError(err)

	srcDataDir := filepath.Join(s.tenantDir, "clone-src", config.DataDir)
	s.Require().NoError(os.WriteFile(
		filepath.Join(srcDataDir, "testfile.txt"),
		[]byte("hello from volume"),
		0o644,
	))

	// Create snapshot
	_, err = s.storage.CreateSnapshot(s.ctx, "test", SnapshotCreateRequest{
		Name: "clone-snap", Volume: "clone-src",
	})
	s.Require().NoError(err)

	// Clone from snapshot
	clone, err := s.storage.CreateClone(s.ctx, "test", CloneCreateRequest{
		Name: "my-clone", Snapshot: "clone-snap",
	})
	s.Require().NoError(err)
	s.Assert().Equal("my-clone", clone.Name)
	s.Assert().Equal("clone-snap", clone.SourceSnapshot)

	// Verify data exists in clone
	cloneDataDir := filepath.Join(s.tenantDir, "my-clone", config.DataDir)
	content, err := os.ReadFile(filepath.Join(cloneDataDir, "testfile.txt"))
	s.Require().NoError(err)
	s.Assert().Equal("hello from volume", string(content))

	// Verify clone is writable (not readonly)
	err = os.WriteFile(filepath.Join(cloneDataDir, "newfile.txt"), []byte("new data"), 0o644)
	s.Assert().NoError(err, "clone should be writable")
}

func (s *StorageIntegrationSuite) TestExportMetadataPersistence() {
	// Use a specific mock exporter for this test
	exporter := &nfs.MockExporter{}
	s.storage.exporter = exporter

	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "export-vol", SizeBytes: 10 * 1024 * 1024,
	})
	s.Require().NoError(err)

	volDir := filepath.Join(s.tenantDir, "export-vol")
	exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil)
	exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(nil)

	// Export
	err = s.storage.ExportVolume(s.ctx, "test", "export-vol", "10.0.0.1")
	s.Require().NoError(err)

	var meta VolumeMetadata
	s.Require().NoError(ReadMetadata(filepath.Join(volDir, config.MetadataFile), &meta))
	s.Assert().Contains(meta.Clients, "10.0.0.1")
	s.Assert().NotNil(meta.LastAttachAt)

	// Unexport
	err = s.storage.UnexportVolume(s.ctx, "test", "export-vol", "10.0.0.1")
	s.Require().NoError(err)

	var afterUnexport VolumeMetadata
	s.Require().NoError(ReadMetadata(filepath.Join(volDir, config.MetadataFile), &afterUnexport))
	s.Assert().NotContains(afterUnexport.Clients, "10.0.0.1")

	exporter.AssertExpectations(s.T())

	// Restore default permissive exporter for remaining tests
	defaultExporter := &nfs.MockExporter{}
	defaultExporter.On("Export", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	defaultExporter.On("Unexport", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	defaultExporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{}, nil).Maybe()
	s.storage.exporter = defaultExporter
}

func (s *StorageIntegrationSuite) TestAlreadyExists() {
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "exists-vol", SizeBytes: 10 * 1024 * 1024,
	})
	s.Require().NoError(err)

	// Create again - should get ErrAlreadyExists with existing metadata
	meta, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "exists-vol", SizeBytes: 20 * 1024 * 1024,
	})
	s.Require().Error(err)
	var se *StorageError
	s.Require().ErrorAs(err, &se)
	s.Assert().Equal(ErrAlreadyExists, se.Code)
	s.Assert().NotNil(meta)
	s.Assert().Equal(uint64(10*1024*1024), meta.SizeBytes, "should return original metadata")
}

func (s *StorageIntegrationSuite) TestDeleteNonexistent() {
	err := s.storage.DeleteVolume(s.ctx, "test", "ghost-vol")
	s.Require().Error(err)
	var se *StorageError
	s.Require().ErrorAs(err, &se)
	s.Assert().Equal(ErrNotFound, se.Code)
}

func (s *StorageIntegrationSuite) TestStartScrub() {
	// write data so scrub has something to check
	_, err := s.storage.CreateVolume(s.ctx, "test", VolumeCreateRequest{
		Name: "scrub-vol", SizeBytes: 10 * 1024 * 1024,
	})
	s.Require().NoError(err)
	dataDir := filepath.Join(s.tenantDir, "scrub-vol", config.DataDir)
	s.Require().NoError(os.WriteFile(filepath.Join(dataDir, "testfile"), make([]byte, 64*1024), 0o644))
	_, err = s.cmd.Run(s.ctx, "sync")
	s.Require().NoError(err)

	// start scrub via storage layer
	taskID, err := s.storage.StartScrub(s.ctx)
	s.Require().NoError(err)
	s.Assert().NotEmpty(taskID)

	// wait for completion
	var tsk *task.Task
	for i := 0; i < 30; i++ {
		tsk, err = s.storage.Tasks().Get(taskID)
		s.Require().NoError(err)
		if tsk.Status == task.TaskCompleted || tsk.Status == task.TaskFailed {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	s.Assert().Equal(task.TaskCompleted, tsk.Status)
	s.Assert().Equal(100, tsk.Progress)
	s.Assert().NotNil(tsk.Result, "should have scrub result")
}

func (s *StorageIntegrationSuite) TestStartScrubDuplicate() {
	// start a blocking scrub in background
	started := make(chan struct{})
	s.storage.Tasks().Create(string(task.TypeScrub), func(ctx context.Context, update *task.Update) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started

	// second scrub should be rejected
	_, err := s.storage.StartScrub(s.ctx)
	s.Require().Error(err)
	var se *StorageError
	s.Require().ErrorAs(err, &se)
	s.Assert().Equal(ErrBusy, se.Code)
}

func (s *StorageIntegrationSuite) TestTaskPersistence() {
	taskDir := filepath.Join(s.mnt, config.TasksDir)

	// submit a task that completes
	id := s.storage.Tasks().Create("test", func(ctx context.Context, update *task.Update) error {
		return update.SetResult(map[string]string{"key": "value"})
	})
	time.Sleep(100 * time.Millisecond)

	// task file should exist
	taskFile := filepath.Join(taskDir, id+".json")
	_, err := os.Stat(taskFile)
	s.Assert().NoError(err, "task file should exist on disk")

	// read the file and verify
	var persisted task.Task
	s.Require().NoError(ReadMetadata(taskFile, &persisted))
	s.Assert().Equal(id, persisted.ID)
	s.Assert().Equal(task.TaskCompleted, persisted.Status)
	s.Assert().NotNil(persisted.Result)
}

func (s *StorageIntegrationSuite) TestTaskLoadFromDisk() {
	taskDir := filepath.Join(s.mnt, config.TasksDir)

	// write a "running" task file (simulates agent crash)
	staleTask := task.Task{
		ID:     "stale-task-123",
		Type:   "test",
		Status: task.TaskRunning,
	}
	s.Require().NoError(writeMetadataAtomic(filepath.Join(taskDir, "stale-task-123.json"), &staleTask))

	// create new TaskManager (triggers loadFromDisk)
	tm := task.NewManager(taskDir, 0)

	// stale task should be marked failed
	tsk, err := tm.Get("stale-task-123")
	s.Require().NoError(err)
	s.Assert().Equal(task.TaskFailed, tsk.Status)
	s.Assert().Equal("agent restarted", tsk.Error)
	s.Assert().NotNil(tsk.CompletedAt)
}

func (s *StorageIntegrationSuite) TestTaskCleanupRemovesFiles() {
	taskDir := filepath.Join(s.mnt, config.TasksDir)

	id := s.storage.Tasks().Create("test", func(ctx context.Context, update *task.Update) error {
		return nil
	})
	time.Sleep(100 * time.Millisecond)

	// file exists
	taskFile := filepath.Join(taskDir, id+".json")
	_, err := os.Stat(taskFile)
	s.Assert().NoError(err)

	// cleanup with 0 maxAge
	s.storage.Tasks().Cleanup(0)

	// file should be gone
	_, err = os.Stat(taskFile)
	s.Assert().True(os.IsNotExist(err), "task file should be removed after cleanup")

	// task should be gone from memory too
	_, err = s.storage.Tasks().Get(id)
	s.Assert().ErrorIs(err, task.ErrNotFound)
}

func (s *StorageIntegrationSuite) TestScrubOnEmptyFilesystem() {
	// no volumes, no data -- scrub should still work
	taskID, err := s.storage.StartScrub(s.ctx)
	s.Require().NoError(err)

	for i := 0; i < 30; i++ {
		tsk, err := s.storage.Tasks().Get(taskID)
		s.Require().NoError(err)
		if tsk.Status == task.TaskCompleted || tsk.Status == task.TaskFailed {
			s.Assert().Equal(task.TaskCompleted, tsk.Status)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *StorageIntegrationSuite) TestScrubRestartRecovery() {
	taskDir := filepath.Join(s.mnt, config.TasksDir)

	// simulate a scrub that was running when agent crashed
	staleTask := task.Task{
		ID:     "stale-scrub",
		Type:   string(task.TypeScrub),
		Status: task.TaskRunning,
	}
	s.Require().NoError(writeMetadataAtomic(filepath.Join(taskDir, "stale-scrub.json"), &staleTask))

	// create new TaskManager (simulates agent restart)
	tm := task.NewManager(taskDir, 0)

	// stale scrub should be failed
	tsk, err := tm.Get("stale-scrub")
	s.Require().NoError(err)
	s.Assert().Equal(task.TaskFailed, tsk.Status)
	s.Assert().Equal("agent restarted", tsk.Error)

	// should be able to start a new scrub (stale one doesn't block)
	s.storage.tasks = tm
	newID, err := s.storage.StartScrub(s.ctx)
	s.Require().NoError(err)
	s.Assert().NotEmpty(newID)

	for i := 0; i < 30; i++ {
		t, _ := tm.Get(newID)
		if t.Status == task.TaskCompleted || t.Status == task.TaskFailed {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}
