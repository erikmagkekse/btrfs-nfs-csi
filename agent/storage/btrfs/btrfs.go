package btrfs

import (
	"context"
	"fmt"
	"strings"
	"syscall"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
)

// TODO: Maybe better scraping? JSON support got added in 6.1 btrfs-progs!
// TODO: Add functionality for squota

type Manager struct {
	bin string
	cmd utils.Runner
}

func NewManager(bin string) *Manager {
	return &Manager{bin: bin, cmd: &utils.ShellRunner{}}
}

func NewManagerWithRunner(bin string, r utils.Runner) *Manager {
	return &Manager{bin: bin, cmd: r}
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
	for line := range strings.SplitSeq(showOut, "\n") {
		trimmed := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(trimmed, "Subvolume ID:"); ok {
			subvolID = strings.TrimSpace(v)
			break
		}
	}
	if subvolID == "" {
		return QgroupInfo{}, fmt.Errorf("subvolume ID not found for %s", path)
	}

	out, err := m.cmd.Run(ctx, m.bin, "qgroup", "show", "-re", "--raw", path)
	if err != nil {
		return QgroupInfo{}, err
	}
	qgroups, err := parseQgroupMap(out)
	if err != nil {
		return QgroupInfo{}, err
	}
	qgroupID := "0/" + subvolID
	info, ok := qgroups[qgroupID]
	if !ok {
		return QgroupInfo{}, fmt.Errorf("qgroup %s not found for %s", qgroupID, path)
	}
	return info, nil
}

func (m *Manager) SetNoCOW(ctx context.Context, path string) error {
	_, err := m.cmd.Run(ctx, "chattr", "+C", path)
	return err
}

func (m *Manager) SetCompression(ctx context.Context, path string, algo string) error {
	if !utils.IsValidCompression(algo) {
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

// Devices returns device info for a btrfs filesystem by parsing
// the output of `btrfs filesystem show`. Returns kernel device paths
// (e.g. /dev/dm-0), not mapper symlinks - works inside containers.
// Devices that are physically absent are marked as Missing.
func (m *Manager) Devices(ctx context.Context, path string) ([]BTRFSDevice, error) {
	out, err := m.cmd.Run(ctx, m.bin, "filesystem", "show", "--raw", path)
	if err != nil {
		return nil, err
	}
	return parseDevices(out)
}

// DeviceErrors runs `btrfs device stats <path>` and parses per-device error counters.
// Output format: [/dev/sda].write_io_errs    0
func (m *Manager) DeviceErrors(ctx context.Context, path string) ([]DeviceErrors, error) {
	out, err := m.cmd.Run(ctx, m.bin, "device", "stats", path)
	if err != nil {
		return nil, err
	}
	return parseDeviceErrors(out)
}

// FilesystemUsage runs `btrfs filesystem usage -b <path>` and parses allocation info.
func (m *Manager) FilesystemUsage(ctx context.Context, path string) (FilesystemUsage, error) {
	out, err := m.cmd.Run(ctx, m.bin, "filesystem", "usage", "-b", path)
	if err != nil {
		return FilesystemUsage{}, err
	}
	return parseFilesystemUsage(out)
}

// ScrubStart runs a btrfs scrub in foreground mode (-B).
// Blocks until the scrub completes or the context is cancelled.
func (m *Manager) ScrubStart(ctx context.Context, path string) error {
	return m.run(ctx, "scrub", "start", "-B", path)
}

// ScrubStatus returns the status of the last/current scrub on the filesystem at path.
func (m *Manager) ScrubStatus(ctx context.Context, path string) (*ScrubStatus, error) {
	out, err := m.cmd.Run(ctx, m.bin, "scrub", "status", "-R", path)
	if err != nil {
		return nil, err
	}
	return parseScrubStatus(out)
}

// QgroupUsageBulk returns qgroup usage for all subvolumes under path.
// Runs `btrfs subvolume list -o` and `btrfs qgroup show -re --raw` once each,
// joins by subvolume ID. Returns map keyed by relative subvolume path.
func (m *Manager) QgroupUsageBulk(ctx context.Context, path string) (map[string]QgroupInfo, error) {
	listOut, err := m.cmd.Run(ctx, m.bin, "subvolume", "list", "-o", path)
	if err != nil {
		return nil, fmt.Errorf("subvolume list: %w", err)
	}
	qgroupOut, err := m.cmd.Run(ctx, m.bin, "qgroup", "show", "-re", "--raw", path)
	if err != nil {
		return nil, fmt.Errorf("qgroup show: %w", err)
	}

	subvols := parseSubvolumeListFull(listOut)
	qgroups, err := parseQgroupMap(qgroupOut)
	if err != nil {
		return nil, err
	}

	result := make(map[string]QgroupInfo, len(subvols))
	for _, sv := range subvols {
		if info, ok := qgroups["0/"+sv.ID]; ok {
			result[sv.Path] = info
		}
	}
	return result, nil
}

func (m *Manager) SubvolumeList(ctx context.Context, path string) ([]SubvolumeInfo, error) {
	out, err := m.cmd.Run(ctx, m.bin, "subvolume", "list", "-o", path)
	if err != nil {
		return nil, err
	}
	entries := parseSubvolumeListFull(out)
	subs := make([]SubvolumeInfo, len(entries))
	for i, e := range entries {
		subs[i] = SubvolumeInfo{Path: e.Path}
	}
	return subs, nil
}
