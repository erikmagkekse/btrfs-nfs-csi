package utils

import (
	"fmt"
	"path/filepath"
	"syscall"
)

// FindMountPoint resolves the mount point for the filesystem containing path
// by walking up the directory tree until the device number changes.
func FindMountPoint(path string) (string, error) {
	const maxDepth = 256

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	var st syscall.Stat_t
	if err := syscall.Stat(absPath, &st); err != nil {
		return "", fmt.Errorf("stat %s: %w", absPath, err)
	}
	dev := st.Dev
	current := absPath
	for range maxDepth {
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		var pst syscall.Stat_t
		if err := syscall.Stat(parent, &pst); err != nil {
			return "", fmt.Errorf("stat %s: %w", parent, err)
		}
		if pst.Dev != dev {
			return current, nil
		}
		current = parent
	}
	return "", fmt.Errorf("mount point not found within %d levels for %s", maxDepth, path)
}

// IsMountPoint returns true if path is a mount point (different device than parent).
func IsMountPoint(path string) bool {
	mp, err := FindMountPoint(path)
	return err == nil && mp == path
}
