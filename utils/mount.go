package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindMountPoint resolves the mount point for the filesystem containing path
// by parsing /proc/self/mountinfo and finding the longest matching mount prefix.
func FindMountPoint(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}

	var bestMount string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mountPoint := fields[4]
		if !strings.HasPrefix(absPath, mountPoint) {
			continue
		}
		// Ensure exact prefix match (not /mnt/btrfs matching /mnt/btr)
		if len(absPath) > len(mountPoint) && absPath[len(mountPoint)] != '/' {
			continue
		}
		if len(mountPoint) > len(bestMount) {
			bestMount = mountPoint
		}
	}

	if bestMount == "" {
		return "", fmt.Errorf("no mount found for %s", absPath)
	}
	return bestMount, nil
}
