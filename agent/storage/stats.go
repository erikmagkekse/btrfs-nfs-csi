package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"

	"github.com/rs/zerolog/log"
)

type DeviceIOStats struct {
	ReadIOs          uint64
	ReadBytes        uint64 // sectors * 512
	ReadTimeMs       uint64
	WriteIOs         uint64
	WriteBytes       uint64 // sectors * 512
	WriteTimeMs      uint64
	IOsInProgress    uint64
	IOTimeMs         uint64
	WeightedIOTimeMs uint64
}

type DeviceStats struct {
	Device     string
	IO         DeviceIOStats
	Errors     btrfs.DeviceErrors
	Filesystem btrfs.FilesystemUsage
}

// resolveBlockDevice finds the block device name for a path by parsing
// /proc/self/mountinfo. This works for btrfs where stat(2) returns virtual
// major:minor (0:XX) that don't exist in /sys/dev/block/.
func resolveBlockDevice(path string) (string, error) {
	return resolveBlockDeviceFrom(path, "/proc/self/mountinfo")
}

func resolveBlockDeviceFrom(path string, mountinfo string) (string, error) {
	data, err := os.ReadFile(mountinfo)
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}

	// Find the longest mount point prefix that matches path.
	// mountinfo format: id parent major:minor root mount_point opts ... - fstype source super_opts
	var bestMount string
	var bestDevice string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		mountPoint := fields[4]
		if !strings.HasPrefix(path, mountPoint) {
			continue
		}
		if len(mountPoint) <= len(bestMount) {
			continue
		}
		// find mount source after the " - " separator
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) {
			continue
		}
		bestMount = mountPoint
		bestDevice = fields[sep+2]
	}

	if bestDevice == "" {
		return "", fmt.Errorf("no mount found for %s", path)
	}

	// bestDevice is e.g. "/dev/sda1" or "/dev/nvme0n1p1"
	name := filepath.Base(bestDevice)

	// If this is a partition, walk up to parent device
	if _, err := os.Stat(filepath.Join("/sys/class/block", name, "partition")); err == nil {
		// read the parent symlink: /sys/class/block/sda1 -> ../devices/.../sda/sda1
		link, err := os.Readlink(filepath.Join("/sys/class/block", name))
		if err == nil {
			name = filepath.Base(filepath.Dir(link))
		}
	}

	return "/dev/" + name, nil
}

// readDeviceIOStats reads /sys/block/<dev>/stat and returns IO counters.
// See https://www.kernel.org/doc/Documentation/block/stat.txt
func readDeviceIOStats(device string) (*DeviceIOStats, error) {
	data, err := os.ReadFile(filepath.Join("/sys/block", filepath.Base(device), "stat"))
	if err != nil {
		return nil, fmt.Errorf("read sysfs stat for %s: %w", device, err)
	}
	return parseDeviceIOStats(string(data))
}

const sectorSize = 512

func parseDeviceIOStats(data string) (*DeviceIOStats, error) {
	fields := strings.Fields(strings.TrimSpace(data))
	if len(fields) < 11 {
		return nil, fmt.Errorf("expected at least 11 fields in sysfs stat, got %d", len(fields))
	}

	vals := make([]uint64, 11)
	for i := 0; i < 11; i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse field %d (%q): %w", i, fields[i], err)
		}
		vals[i] = v
	}

	return &DeviceIOStats{
		ReadIOs:          vals[0],
		ReadBytes:        vals[2] * sectorSize, // sectors read
		ReadTimeMs:       vals[3],
		WriteIOs:         vals[4],
		WriteBytes:       vals[6] * sectorSize, // sectors written
		WriteTimeMs:      vals[7],
		IOsInProgress:    vals[8],
		IOTimeMs:         vals[9],
		WeightedIOTimeMs: vals[10],
	}, nil
}

// StartDeviceIOUpdater polls sysfs IO counters at a high frequency (default 5s).
func (s *Storage) StartDeviceIOUpdater(ctx context.Context, interval time.Duration) {
	go func() {
		s.updateDeviceIO()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.updateDeviceIO()
			}
		}
	}()
}

func (s *Storage) updateDeviceIO() {
	device, err := resolveBlockDevice(s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device io updater: failed to resolve block device")
		return
	}

	io, err := readDeviceIOStats(device)
	if err != nil {
		log.Warn().Err(err).Msg("device io updater: failed to read IO stats")
		return
	}

	DeviceReadBytesTotal.WithLabelValues(device).Set(float64(io.ReadBytes))
	DeviceReadIOsTotal.WithLabelValues(device).Set(float64(io.ReadIOs))
	DeviceReadTimeSecondsTotal.WithLabelValues(device).Set(float64(io.ReadTimeMs) / 1000.0)
	DeviceWriteBytesTotal.WithLabelValues(device).Set(float64(io.WriteBytes))
	DeviceWriteIOsTotal.WithLabelValues(device).Set(float64(io.WriteIOs))
	DeviceWriteTimeSecondsTotal.WithLabelValues(device).Set(float64(io.WriteTimeMs) / 1000.0)
	DeviceIOsInProgress.WithLabelValues(device).Set(float64(io.IOsInProgress))
	DeviceIOTimeSecondsTotal.WithLabelValues(device).Set(float64(io.IOTimeMs) / 1000.0)
	DeviceIOWeightedTimeSecondsTotal.WithLabelValues(device).Set(float64(io.WeightedIOTimeMs) / 1000.0)
}

// StartDeviceStatsUpdater polls btrfs device errors and filesystem usage (default 1m).
func (s *Storage) StartDeviceStatsUpdater(ctx context.Context, interval time.Duration) {
	go func() {
		s.updateBtrfsStats(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.updateBtrfsStats(ctx)
			}
		}
	}()
}

func (s *Storage) updateBtrfsStats(ctx context.Context) {
	device, err := resolveBlockDevice(s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device stats updater: failed to resolve block device")
		return
	}

	de, err := s.btrfs.DeviceErrors(ctx, s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device stats updater: btrfs device errors failed")
	} else {
		DeviceReadErrsTotal.WithLabelValues(device).Set(float64(de.ReadErrs))
		DeviceWriteErrsTotal.WithLabelValues(device).Set(float64(de.WriteErrs))
		DeviceFlushErrsTotal.WithLabelValues(device).Set(float64(de.FlushErrs))
		DeviceCorruptionErrsTotal.WithLabelValues(device).Set(float64(de.CorruptionErrs))
		DeviceGenerationErrsTotal.WithLabelValues(device).Set(float64(de.GenerationErrs))
	}

	fu, err := s.btrfs.FilesystemUsage(ctx, s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device stats updater: btrfs filesystem usage failed")
	} else {
		FilesystemSizeBytes.WithLabelValues(device).Set(float64(fu.TotalBytes))
		FilesystemUsedBytes.WithLabelValues(device).Set(float64(fu.UsedBytes))
		FilesystemUnallocatedBytes.WithLabelValues(device).Set(float64(fu.UnallocatedBytes))
		FilesystemMetadataUsedBytes.WithLabelValues(device).Set(float64(fu.MetadataUsedBytes))
		FilesystemMetadataTotalBytes.WithLabelValues(device).Set(float64(fu.MetadataTotalBytes))
		FilesystemDataRatio.WithLabelValues(device).Set(fu.DataRatio)
	}

	log.Debug().Str("device", device).Msg("device stats updater: metrics updated")
}

// DeviceStats collects device IO, btrfs errors, and filesystem usage for the base path.
func (s *Storage) DeviceStats(ctx context.Context) (*DeviceStats, error) {
	device, err := resolveBlockDevice(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("resolve block device: %w", err)
	}

	io, err := readDeviceIOStats(device)
	if err != nil {
		return nil, fmt.Errorf("read device IO stats: %w", err)
	}

	de, err := s.btrfs.DeviceErrors(ctx, s.basePath)
	if err != nil {
		return nil, fmt.Errorf("btrfs device errors: %w", err)
	}

	fu, err := s.btrfs.FilesystemUsage(ctx, s.basePath)
	if err != nil {
		return nil, fmt.Errorf("btrfs filesystem usage: %w", err)
	}

	return &DeviceStats{
		Device:     device,
		IO:         *io,
		Errors:     de,
		Filesystem: fu,
	}, nil
}
