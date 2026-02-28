package btrfs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"syscall"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
)

// TODO: Maybe better scraping?
// TODO: Add functionality for squota

type SubvolumeInfo struct {
	Path string
}

type Manager struct {
	bin string
	cmd utils.Runner
}

func NewManager(bin string) *Manager {
	return &Manager{bin: bin, cmd: &utils.ShellRunner{}}
}

func (m *Manager) SubvolumeCreate(ctx context.Context, path string) error {
	return m.run(ctx, "subvolume", "create", path)
}

func (m *Manager) SubvolumeDelete(ctx context.Context, path string) error {
	return m.run(ctx, "subvolume", "delete", path)
}

func (m *Manager) SubvolumeSnapshot(ctx context.Context, src, dst string, readonly bool) error {
	if readonly {
		return m.run(ctx, "subvolume", "snapshot", "-r", src, dst)
	}
	return m.run(ctx, "subvolume", "snapshot", src, dst)
}

// QuotaCheck verifies that btrfs quota is enabled on the filesystem.
func (m *Manager) QuotaCheck(ctx context.Context, path string) error {
	return m.run(ctx, "qgroup", "show", path)
}

func (m *Manager) QgroupLimit(ctx context.Context, path string, bytes uint64) error {
	return m.run(ctx, "qgroup", "limit", fmt.Sprintf("%d", bytes), path)
}

type QgroupInfo struct {
	Referenced uint64
	Exclusive  uint64
}

// QgroupUsage returns the referenced bytes used by the subvolume's qgroup.
func (m *Manager) QgroupUsage(ctx context.Context, path string) (uint64, error) {
	info, err := m.QgroupUsageEx(ctx, path)
	if err != nil {
		return 0, err
	}
	return info.Referenced, nil
}

// QgroupUsageEx returns both referenced and exclusive bytes for the subvolume's qgroup.
func (m *Manager) QgroupUsageEx(ctx context.Context, path string) (QgroupInfo, error) {
	// get subvolume ID to find the correct qgroup
	showOut, err := m.cmd.Run(ctx, m.bin, "subvolume", "show", path)
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

	out, err := m.cmd.Run(ctx, m.bin, "qgroup", "show", "-re", "--raw", path)
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

func (m *Manager) SetNoCOW(ctx context.Context, path string) error {
	_, err := m.cmd.Run(ctx, "chattr", "+C", path)
	return err
}

var validCompressionAlgo = map[string]bool{
	"zstd": true,
	"lzo":  true,
	"zlib": true,
}

func IsValidCompression(s string) bool {
	if s == "" || s == "none" {
		return true
	}
	parts := strings.SplitN(s, ":", 2)
	if !validCompressionAlgo[parts[0]] {
		return false
	}
	if len(parts) == 2 {
		level, err := strconv.Atoi(parts[1])
		if err != nil || level < 1 || level > 15 {
			return false
		}
	}
	return true
}

func (m *Manager) SetCompression(ctx context.Context, path string, algo string) error {
	if !IsValidCompression(algo) {
		return fmt.Errorf("invalid compression algorithm: %s", algo)
	}
	return m.run(ctx, "property", "set", path, "compression", algo)
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

func (m *Manager) IsAvailable(ctx context.Context) bool {
	err := m.run(ctx, "--version")
	return err == nil
}

func (m *Manager) run(ctx context.Context, args ...string) error {
	_, err := m.cmd.Run(ctx, m.bin, args...)
	return err
}

// TODO: use SubvolumeExists and SubvolumeList for a periodic consistency check
// that compares metadata.json entries against actual btrfs subvolumes.
// Should be fine since we anyways just list subvols for this specific path.
func (m *Manager) SubvolumeExists(ctx context.Context, path string) bool {
	err := m.run(ctx, "subvolume", "show", path)
	return err == nil
}

func (m *Manager) SubvolumeList(ctx context.Context, path string) ([]SubvolumeInfo, error) {
	out, err := m.cmd.Run(ctx, m.bin, "subvolume", "list", "-o", path)
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
