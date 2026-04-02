//go:build integration

package btrfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/suite"
)

type BtrfsIntegrationSuite struct {
	suite.Suite
	mnt     string
	mgr     *Manager
	cmd     *utils.ShellRunner
	ctx     context.Context
	loopDev string
}

func TestBtrfsIntegration(t *testing.T) {
	suite.Run(t, new(BtrfsIntegrationSuite))
}

func (s *BtrfsIntegrationSuite) SetupSuite() {
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

	s.mgr = NewManager("btrfs")
}

func (s *BtrfsIntegrationSuite) TearDownSuite() {
	_, _ = s.cmd.Run(s.ctx, "umount", s.mnt)
	_, _ = s.cmd.Run(s.ctx, "losetup", "-d", s.loopDev)
}

func (s *BtrfsIntegrationSuite) TestSubvolumeCreateDelete() {
	subPath := filepath.Join(s.mnt, "testvol")

	err := s.mgr.SubvolumeCreate(s.ctx, subPath)
	s.Require().NoError(err, "SubvolumeCreate")
	s.Assert().True(s.mgr.SubvolumeExists(s.ctx, subPath), "should exist after create")

	err = s.mgr.SubvolumeDelete(s.ctx, subPath)
	s.Require().NoError(err, "SubvolumeDelete")
	s.Assert().False(s.mgr.SubvolumeExists(s.ctx, subPath), "should not exist after delete")
}

func (s *BtrfsIntegrationSuite) TestSnapshot() {
	src := filepath.Join(s.mnt, "srcvol-snap")
	rwSnap := filepath.Join(s.mnt, "rwsnap")
	roSnap := filepath.Join(s.mnt, "rosnap")

	err := s.mgr.SubvolumeCreate(s.ctx, src)
	s.Require().NoError(err, "SubvolumeCreate")

	err = s.mgr.SubvolumeSnapshot(s.ctx, src, rwSnap, false)
	s.Require().NoError(err, "SubvolumeSnapshot(rw)")
	s.Assert().True(s.mgr.SubvolumeExists(s.ctx, rwSnap), "rw snapshot should exist")

	err = s.mgr.SubvolumeSnapshot(s.ctx, src, roSnap, true)
	s.Require().NoError(err, "SubvolumeSnapshot(ro)")
	s.Assert().True(s.mgr.SubvolumeExists(s.ctx, roSnap), "ro snapshot should exist")
}

func (s *BtrfsIntegrationSuite) TestQgroupLimit() {
	subPath := filepath.Join(s.mnt, "quotavol")
	err := s.mgr.SubvolumeCreate(s.ctx, subPath)
	s.Require().NoError(err, "SubvolumeCreate")

	err = s.mgr.QgroupLimit(s.ctx, subPath, 10*1024*1024)
	s.Require().NoError(err, "QgroupLimit")

	data := make([]byte, 64*1024)
	err = os.WriteFile(filepath.Join(subPath, "testfile"), data, 0o644)
	s.Require().NoError(err, "WriteFile")

	_, err = s.cmd.Run(s.ctx, "sync")
	s.Require().NoError(err, "sync")

	used, err := s.mgr.QgroupUsage(s.ctx, subPath)
	s.Require().NoError(err, "QgroupUsage")
	s.Assert().NotZero(used, "QgroupUsage should be > 0 after writing data")
}

func (s *BtrfsIntegrationSuite) TestQgroupUsageEx() {
	subPath := filepath.Join(s.mnt, "usagevol")
	err := s.mgr.SubvolumeCreate(s.ctx, subPath)
	s.Require().NoError(err, "SubvolumeCreate")

	data := make([]byte, 128*1024)
	err = os.WriteFile(filepath.Join(subPath, "testfile"), data, 0o644)
	s.Require().NoError(err, "WriteFile")

	_, err = s.cmd.Run(s.ctx, "sync")
	s.Require().NoError(err, "sync")

	info, err := s.mgr.QgroupUsageEx(s.ctx, subPath)
	s.Require().NoError(err, "QgroupUsageEx")
	s.Assert().NotZero(info.Referenced, "Referenced should be > 0")
	s.Assert().NotZero(info.Exclusive, "Exclusive should be > 0")
}

func (s *BtrfsIntegrationSuite) TestSubvolumeList() {
	vol1 := filepath.Join(s.mnt, "listvol1")
	vol2 := filepath.Join(s.mnt, "listvol2")

	err := s.mgr.SubvolumeCreate(s.ctx, vol1)
	s.Require().NoError(err, "SubvolumeCreate(vol1)")
	err = s.mgr.SubvolumeCreate(s.ctx, vol2)
	s.Require().NoError(err, "SubvolumeCreate(vol2)")

	subs, err := s.mgr.SubvolumeList(s.ctx, s.mnt)
	s.Require().NoError(err, "SubvolumeList")

	paths := make(map[string]bool)
	for _, sub := range subs {
		paths[sub.Path] = true
	}
	s.Assert().True(paths["listvol1"], "listvol1 should be in list")
	s.Assert().True(paths["listvol2"], "listvol2 should be in list")
}

func (s *BtrfsIntegrationSuite) TestSetCompression() {
	subPath := filepath.Join(s.mnt, "compvol")
	err := s.mgr.SubvolumeCreate(s.ctx, subPath)
	s.Require().NoError(err, "SubvolumeCreate")

	err = s.mgr.SetCompression(s.ctx, subPath, "zstd")
	s.Require().NoError(err, "SetCompression")

	out, err := s.cmd.Run(s.ctx, "btrfs", "property", "get", subPath, "compression")
	s.Require().NoError(err, "property get")
	s.Assert().Contains(out, "zstd")
}

func (s *BtrfsIntegrationSuite) TestConcurrentCreate() {
	errs := make(chan error, 31)
	for i := range 31 {
		go func() {
			name := fmt.Sprintf("cc-vol-%02d", i)
			errs <- s.mgr.SubvolumeCreate(s.ctx, filepath.Join(s.mnt, name))
		}()
	}
	for range 31 {
		s.Assert().NoError(<-errs, "SubvolumeCreate")
	}

	subs, err := s.mgr.SubvolumeList(s.ctx, s.mnt)
	s.Require().NoError(err, "SubvolumeList")

	count := 0
	for _, sub := range subs {
		if strings.HasPrefix(sub.Path, "cc-vol-") {
			count++
		}
	}
	s.Assert().Equal(31, count, "expected 31 cc-vol-* subvolumes")
}

func (s *BtrfsIntegrationSuite) TestConcurrentDelete() {
	for i := range 31 {
		name := fmt.Sprintf("cd-vol-%02d", i)
		err := s.mgr.SubvolumeCreate(s.ctx, filepath.Join(s.mnt, name))
		s.Require().NoError(err, "SubvolumeCreate(%s)", name)
	}

	errs := make(chan error, 31)
	for i := range 31 {
		go func() {
			name := fmt.Sprintf("cd-vol-%02d", i)
			errs <- s.mgr.SubvolumeDelete(s.ctx, filepath.Join(s.mnt, name))
		}()
	}
	for range 31 {
		s.Assert().NoError(<-errs, "SubvolumeDelete")
	}

	subs, err := s.mgr.SubvolumeList(s.ctx, s.mnt)
	s.Require().NoError(err, "SubvolumeList")

	for _, sub := range subs {
		s.Assert().False(strings.HasPrefix(sub.Path, "cd-vol-"), "cd-vol-* should be deleted, found %s", sub.Path)
	}
}

func (s *BtrfsIntegrationSuite) TestConcurrentSnapshot() {
	src := filepath.Join(s.mnt, "cs-srcvol")
	err := s.mgr.SubvolumeCreate(s.ctx, src)
	s.Require().NoError(err, "SubvolumeCreate")

	errs := make(chan error, 31)
	for i := range 31 {
		go func() {
			name := fmt.Sprintf("cs-snap-%02d", i)
			errs <- s.mgr.SubvolumeSnapshot(s.ctx, src, filepath.Join(s.mnt, name), true)
		}()
	}
	for range 31 {
		s.Assert().NoError(<-errs, "SubvolumeSnapshot")
	}

	subs, err := s.mgr.SubvolumeList(s.ctx, s.mnt)
	s.Require().NoError(err, "SubvolumeList")

	snapCount := 0
	for _, sub := range subs {
		if strings.HasPrefix(sub.Path, "cs-snap-") {
			snapCount++
		}
	}
	s.Assert().Equal(31, snapCount, "expected 31 cs-snap-* snapshots")
}

func (s *BtrfsIntegrationSuite) TestScrubStartAndStatus() {
	// write some data so scrub has something to check
	subPath := filepath.Join(s.mnt, "scrubvol")
	err := s.mgr.SubvolumeCreate(s.ctx, subPath)
	s.Require().NoError(err, "SubvolumeCreate")

	data := make([]byte, 256*1024)
	err = os.WriteFile(filepath.Join(subPath, "testfile"), data, 0o644)
	s.Require().NoError(err, "WriteFile")

	_, err = s.cmd.Run(s.ctx, "sync")
	s.Require().NoError(err, "sync")

	// run scrub in foreground (-B)
	err = s.mgr.ScrubStart(s.ctx, s.mnt)
	s.Require().NoError(err, "ScrubStart")

	// check result
	status, err := s.mgr.ScrubStatus(s.ctx, s.mnt)
	s.Require().NoError(err, "ScrubStatus")
	s.Assert().False(status.Running, "scrub should be finished")
	s.Assert().NotZero(status.DataBytesScrubbed, "should have scrubbed some data bytes")
	s.Assert().Equal(uint64(0), status.ReadErrors, "no read errors expected")
	s.Assert().Equal(uint64(0), status.CSumErrors, "no csum errors expected")
	s.Assert().Equal(uint64(0), status.UncorrectableErrs, "no uncorrectable errors expected")
}

func (s *BtrfsIntegrationSuite) TestScrubCancelViaContext() {
	ctx, cancel := context.WithCancel(s.ctx)

	done := make(chan error, 1)
	go func() {
		done <- s.mgr.ScrubStart(ctx, s.mnt)
	}()

	// cancel immediately
	cancel()

	err := <-done
	// either the scrub finished before cancel (small filesystem) or was killed
	if err != nil {
		s.Assert().ErrorIs(err, context.Canceled, "should be context.Canceled or wrapped")
	}
}
