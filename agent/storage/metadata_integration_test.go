//go:build integration

// This test file also contains tests for Types like VolumeMetadata, kinda.

package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/suite"
)

type UtilsIntegrationSuite struct {
	suite.Suite
	mnt     string
	cmd     *utils.ShellRunner
	ctx     context.Context
	loopDev string
}

func TestUtilsIntegration(t *testing.T) {
	suite.Run(t, new(UtilsIntegrationSuite))
}

func (s *UtilsIntegrationSuite) SetupSuite() {
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
}

func (s *UtilsIntegrationSuite) TearDownSuite() {
	_, _ = s.cmd.Run(s.ctx, "umount", s.mnt)
	_, _ = s.cmd.Run(s.ctx, "losetup", "-d", s.loopDev)
}

func (s *UtilsIntegrationSuite) TestWriteReadMetadata() {
	path := filepath.Join(s.mnt, "meta.json")

	now := time.Now().Truncate(time.Second).UTC()
	written := VolumeMetadata{
		Name:        "testvol",
		Path:        "/data/testvol",
		SizeBytes:   1024 * 1024,
		NoCOW:       true,
		Compression: "zstd",
		QuotaBytes:  2 * 1024 * 1024,
		UID:         1000,
		GID:         1000,
		Mode:        "0755",
		Clients:     []string{"10.0.0.1"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.Require().NoError(writeMetadataAtomic(path, &written))

	var read VolumeMetadata
	s.Require().NoError(ReadMetadata(path, &read))

	s.Assert().Equal(written, read)

	// Verify no .tmp file remains (atomic rename)
	_, err := os.Stat(path + ".tmp")
	s.Assert().True(os.IsNotExist(err), ".tmp file should not remain after atomic write")
}

func (s *UtilsIntegrationSuite) TestReadMetadataNotFound() {
	path := filepath.Join(s.mnt, "nonexistent.json")

	var meta VolumeMetadata
	err := ReadMetadata(path, &meta)
	s.Require().Error(err)
	s.Assert().True(os.IsNotExist(err))
}

func (s *UtilsIntegrationSuite) TestUpdateMetadata() {
	path := filepath.Join(s.mnt, "update-meta.json")

	initial := VolumeMetadata{
		Name:      "testvol",
		Path:      "/data/testvol",
		SizeBytes: 1024,
	}
	s.Require().NoError(writeMetadataAtomic(path, &initial))

	err := UpdateMetadata(path, func(m *VolumeMetadata) {
		m.SizeBytes = 2048
		m.Compression = "zstd"
	})
	s.Require().NoError(err)

	var updated VolumeMetadata
	s.Require().NoError(ReadMetadata(path, &updated))

	s.Assert().Equal("testvol", updated.Name)
	s.Assert().Equal(uint64(2048), updated.SizeBytes)
	s.Assert().Equal("zstd", updated.Compression)
}

func (s *UtilsIntegrationSuite) TestWriteMetadataAtomicInvalidPath() {
	path := filepath.Join(s.mnt, "nonexistent", "subdir", "meta.json")

	meta := VolumeMetadata{Name: "testvol"}
	err := writeMetadataAtomic(path, &meta)
	s.Require().Error(err)
}

func (s *UtilsIntegrationSuite) TestUpdateMetadataConcurrent() {
	path := filepath.Join(s.mnt, "concurrent-meta.json")

	initial := VolumeMetadata{
		Name:      "testvol",
		Path:      "/data/testvol",
		UsedBytes: 0,
	}
	s.Require().NoError(writeMetadataAtomic(path, &initial))

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := UpdateMetadata(path, func(m *VolumeMetadata) {
				m.UsedBytes++
			})
			s.Assert().NoError(err)
		}()
	}
	wg.Wait()

	var result VolumeMetadata
	s.Require().NoError(ReadMetadata(path, &result))
	s.Assert().Equal(uint64(goroutines), result.UsedBytes, "expected no lost updates")
}
