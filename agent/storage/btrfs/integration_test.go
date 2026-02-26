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
)

// setupLoopBtrfs creates a loop-mounted btrfs filesystem with quotas enabled.
// Requires root and btrfs-progs.
func setupLoopBtrfs(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	cmd := &utils.ShellRunner{}

	tmpDir := t.TempDir()
	imgFile := filepath.Join(tmpDir, "btrfs.img")
	mountDir := filepath.Join(tmpDir, "mnt")

	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// sparse file
	if _, err := cmd.Run(ctx, "fallocate", "-l", "256M", imgFile); err != nil {
		t.Fatalf("fallocate: %v", err)
	}

	// loop device
	loopOut, err := cmd.Run(ctx, "losetup", "--find", "--show", imgFile)
	if err != nil {
		t.Fatalf("losetup: %v", err)
	}
	loopDev := strings.TrimSpace(loopOut)

	// mkfs.btrfs
	if _, err := cmd.Run(ctx, "mkfs.btrfs", "-f", loopDev); err != nil {
		// cleanup loop on failure
		_, _ = cmd.Run(ctx, "losetup", "-d", loopDev)
		t.Fatalf("mkfs.btrfs: %v", err)
	}

	// mount
	if _, err := cmd.Run(ctx, "mount", loopDev, mountDir); err != nil {
		_, _ = cmd.Run(ctx, "losetup", "-d", loopDev)
		t.Fatalf("mount: %v", err)
	}

	// enable quotas
	if _, err := cmd.Run(ctx, "btrfs", "quota", "enable", mountDir); err != nil {
		_, _ = cmd.Run(ctx, "umount", mountDir)
		_, _ = cmd.Run(ctx, "losetup", "-d", loopDev)
		t.Fatalf("quota enable: %v", err)
	}

	t.Cleanup(func() {
		_, _ = cmd.Run(ctx, "umount", mountDir)
		_, _ = cmd.Run(ctx, "losetup", "-d", loopDev)
	})

	return mountDir
}

func TestIntegrationSubvolumeCreateDelete(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	subPath := filepath.Join(mnt, "testvol")

	if err := mgr.SubvolumeCreate(ctx, subPath); err != nil {
		t.Fatalf("SubvolumeCreate: %v", err)
	}
	if !mgr.SubvolumeExists(ctx, subPath) {
		t.Fatal("SubvolumeExists should return true after create")
	}

	if err := mgr.SubvolumeDelete(ctx, subPath); err != nil {
		t.Fatalf("SubvolumeDelete: %v", err)
	}
	if mgr.SubvolumeExists(ctx, subPath) {
		t.Fatal("SubvolumeExists should return false after delete")
	}
}

func TestIntegrationSnapshot(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	src := filepath.Join(mnt, "srcvol")
	rwSnap := filepath.Join(mnt, "rwsnap")
	roSnap := filepath.Join(mnt, "rosnap")

	if err := mgr.SubvolumeCreate(ctx, src); err != nil {
		t.Fatalf("SubvolumeCreate: %v", err)
	}

	if err := mgr.SubvolumeSnapshot(ctx, src, rwSnap, false); err != nil {
		t.Fatalf("SubvolumeSnapshot(rw): %v", err)
	}
	if !mgr.SubvolumeExists(ctx, rwSnap) {
		t.Fatal("rw snapshot should exist")
	}

	if err := mgr.SubvolumeSnapshot(ctx, src, roSnap, true); err != nil {
		t.Fatalf("SubvolumeSnapshot(ro): %v", err)
	}
	if !mgr.SubvolumeExists(ctx, roSnap) {
		t.Fatal("ro snapshot should exist")
	}
}

func TestIntegrationQgroupLimit(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	subPath := filepath.Join(mnt, "quotavol")
	if err := mgr.SubvolumeCreate(ctx, subPath); err != nil {
		t.Fatalf("SubvolumeCreate: %v", err)
	}

	// set 10MB limit
	if err := mgr.QgroupLimit(ctx, subPath, 10*1024*1024); err != nil {
		t.Fatalf("QgroupLimit: %v", err)
	}

	// write some data
	data := make([]byte, 64*1024)
	if err := os.WriteFile(filepath.Join(subPath, "testfile"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// sync filesystem so qgroup accounting is up to date
	cmd := &utils.ShellRunner{}
	if _, err := cmd.Run(ctx, "sync"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	used, err := mgr.QgroupUsage(ctx, subPath)
	if err != nil {
		t.Fatalf("QgroupUsage: %v", err)
	}
	if used == 0 {
		t.Error("QgroupUsage should be > 0 after writing data")
	}
}

func TestIntegrationQgroupUsageEx(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	subPath := filepath.Join(mnt, "usagevol")
	if err := mgr.SubvolumeCreate(ctx, subPath); err != nil {
		t.Fatalf("SubvolumeCreate: %v", err)
	}

	// write data
	data := make([]byte, 128*1024)
	if err := os.WriteFile(filepath.Join(subPath, "testfile"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// sync filesystem so qgroup accounting is up to date
	cmd := &utils.ShellRunner{}
	if _, err := cmd.Run(ctx, "sync"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	info, err := mgr.QgroupUsageEx(ctx, subPath)
	if err != nil {
		t.Fatalf("QgroupUsageEx: %v", err)
	}
	if info.Referenced == 0 {
		t.Error("Referenced should be > 0")
	}
	if info.Exclusive == 0 {
		t.Error("Exclusive should be > 0")
	}
}

func TestIntegrationSubvolumeList(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	vol1 := filepath.Join(mnt, "listvol1")
	vol2 := filepath.Join(mnt, "listvol2")

	if err := mgr.SubvolumeCreate(ctx, vol1); err != nil {
		t.Fatalf("SubvolumeCreate(vol1): %v", err)
	}
	if err := mgr.SubvolumeCreate(ctx, vol2); err != nil {
		t.Fatalf("SubvolumeCreate(vol2): %v", err)
	}

	subs, err := mgr.SubvolumeList(ctx, mnt)
	if err != nil {
		t.Fatalf("SubvolumeList: %v", err)
	}

	if len(subs) != 2 {
		t.Fatalf("expected 2 subvolumes, got %d", len(subs))
	}

	paths := make(map[string]bool)
	for _, s := range subs {
		paths[s.Path] = true
	}
	if !paths["listvol1"] && !paths["listvol2"] {
		t.Errorf("unexpected paths: %v", subs)
	}
}

func TestIntegrationSetCompression(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()
	cmd := &utils.ShellRunner{}

	subPath := filepath.Join(mnt, "compvol")
	if err := mgr.SubvolumeCreate(ctx, subPath); err != nil {
		t.Fatalf("SubvolumeCreate: %v", err)
	}

	if err := mgr.SetCompression(ctx, subPath, "zstd"); err != nil {
		t.Fatalf("SetCompression: %v", err)
	}

	out, err := cmd.Run(ctx, "btrfs", "property", "get", subPath, "compression")
	if err != nil {
		t.Fatalf("property get: %v", err)
	}
	if !strings.Contains(out, "zstd") {
		t.Errorf("expected compression=zstd, got: %s", strings.TrimSpace(out))
	}
}

// not sure how useful they are but who knows :D
func TestIntegrationConcurrentCreate(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	errs := make(chan error, 31)
	for i := range 31 {
		go func() {
			name := fmt.Sprintf("vol-%02d", i)
			errs <- mgr.SubvolumeCreate(ctx, filepath.Join(mnt, name))
		}()
	}
	for range 31 {
		if err := <-errs; err != nil {
			t.Errorf("SubvolumeCreate: %v", err)
		}
	}

	subs, err := mgr.SubvolumeList(ctx, mnt)
	if err != nil {
		t.Fatalf("SubvolumeList: %v", err)
	}
	if len(subs) != 31 {
		t.Errorf("expected 31 subvolumes, got %d", len(subs))
	}
}

func TestIntegrationConcurrentDelete(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	// create 31 subvolumes sequentially
	for i := range 31 {
		name := fmt.Sprintf("vol-%02d", i)
		if err := mgr.SubvolumeCreate(ctx, filepath.Join(mnt, name)); err != nil {
			t.Fatalf("SubvolumeCreate(%s): %v", name, err)
		}
	}

	// delete all concurrently
	errs := make(chan error, 31)
	for i := range 31 {
		go func() {
			name := fmt.Sprintf("vol-%02d", i)
			errs <- mgr.SubvolumeDelete(ctx, filepath.Join(mnt, name))
		}()
	}
	for range 31 {
		if err := <-errs; err != nil {
			t.Errorf("SubvolumeDelete: %v", err)
		}
	}

	subs, err := mgr.SubvolumeList(ctx, mnt)
	if err != nil {
		t.Fatalf("SubvolumeList: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected 0 subvolumes, got %d", len(subs))
	}
}

func TestIntegrationConcurrentSnapshot(t *testing.T) {
	mnt := setupLoopBtrfs(t)
	mgr := NewManager()
	ctx := context.Background()

	src := filepath.Join(mnt, "srcvol")
	if err := mgr.SubvolumeCreate(ctx, src); err != nil {
		t.Fatalf("SubvolumeCreate: %v", err)
	}

	// 31 concurrent snapshots from same source
	errs := make(chan error, 31)
	for i := range 31 {
		go func() {
			name := fmt.Sprintf("snap-%02d", i)
			errs <- mgr.SubvolumeSnapshot(ctx, src, filepath.Join(mnt, name), true)
		}()
	}
	for range 31 {
		if err := <-errs; err != nil {
			t.Errorf("SubvolumeSnapshot: %v", err)
		}
	}

	// +1 for srcvol itself
	subs, err := mgr.SubvolumeList(ctx, mnt)
	if err != nil {
		t.Fatalf("SubvolumeList: %v", err)
	}
	if len(subs) != 32 {
		t.Errorf("expected 32 subvolumes (1 src + 31 snaps), got %d", len(subs))
	}
}
