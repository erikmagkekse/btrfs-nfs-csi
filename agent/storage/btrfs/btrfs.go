package btrfs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

type SubvolumeInfo struct {
	Path string
}

func SubvolumeCreate(ctx context.Context, path string) error {
	return run(ctx, "btrfs", "subvolume", "create", path)
}

func SubvolumeDelete(ctx context.Context, path string) error {
	return run(ctx, "btrfs", "subvolume", "delete", path)
}

func SubvolumeSnapshot(ctx context.Context, src, dst string, readonly bool) error {
	if readonly {
		return run(ctx, "btrfs", "subvolume", "snapshot", "-r", src, dst)
	}
	return run(ctx, "btrfs", "subvolume", "snapshot", src, dst)
}

func SubvolumeExists(ctx context.Context, path string) bool {
	err := run(ctx, "btrfs", "subvolume", "show", path)
	return err == nil
}

func SubvolumeList(ctx context.Context, path string) ([]SubvolumeInfo, error) {
	out, err := output(ctx, "btrfs", "subvolume", "list", "-o", path)
	if err != nil {
		return nil, err
	}

	var subs []SubvolumeInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// format: ID <id> gen <gen> top level <tl> path <path>
		parts := strings.Fields(line)
		if len(parts) >= 9 {
			subs = append(subs, SubvolumeInfo{Path: parts[8]})
		}
	}
	return subs, nil
}

// QuotaCheck verifies that btrfs quota is enabled on the filesystem.
func QuotaCheck(ctx context.Context, path string) error {
	return run(ctx, "btrfs", "qgroup", "show", path)
}

func QgroupLimit(ctx context.Context, path string, bytes uint64) error {
	return run(ctx, "btrfs", "qgroup", "limit", fmt.Sprintf("%d", bytes), path)
}

type QgroupInfo struct {
	Referenced uint64
	Exclusive  uint64
}

// QgroupUsage returns the referenced bytes used by the subvolume's qgroup.
func QgroupUsage(ctx context.Context, path string) (uint64, error) {
	info, err := QgroupUsageEx(ctx, path)
	if err != nil {
		return 0, err
	}
	return info.Referenced, nil
}

// QgroupUsageEx returns both referenced and exclusive bytes for the subvolume's qgroup.
func QgroupUsageEx(ctx context.Context, path string) (QgroupInfo, error) {
	// get subvolume ID to find the correct qgroup
	showOut, err := output(ctx, "btrfs", "subvolume", "show", path)
	if err != nil {
		return QgroupInfo{}, err
	}
	var subvolID string
	for _, line := range strings.Split(showOut, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Subvolume ID:") {
			subvolID = strings.TrimSpace(strings.TrimPrefix(trimmed, "Subvolume ID:"))
			break
		}
	}
	if subvolID == "" {
		return QgroupInfo{}, fmt.Errorf("subvolume ID not found for %s", path)
	}

	qgroupID := "0/" + subvolID

	out, err := output(ctx, "btrfs", "qgroup", "show", "-re", "--raw", path)
	if err != nil {
		return QgroupInfo{}, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == qgroupID {
			var info QgroupInfo
			_, _ = fmt.Sscanf(fields[1], "%d", &info.Referenced)
			_, _ = fmt.Sscanf(fields[2], "%d", &info.Exclusive)
			return info, nil
		}
	}
	return QgroupInfo{}, fmt.Errorf("qgroup %s not found for %s", qgroupID, path)
}

func SetNoCOW(ctx context.Context, path string) error {
	return run(ctx, "chattr", "+C", path)
}

func SetCompression(ctx context.Context, path string, algo string) error {
	return run(ctx, "btrfs", "property", "set", path, "compression", algo)
}

// IsBtrfs checks whether the given path resides on a btrfs filesystem
// by inspecting the filesystem magic number via statfs(2).
func IsBtrfs(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	// btrfs magic: 0x9123683E
	return st.Type == 0x9123683E
}

func IsAvailable(ctx context.Context) bool {
	err := run(ctx, "btrfs", "--version")
	return err == nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
