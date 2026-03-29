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

type PerDeviceStats struct {
	Device string
	IO     DeviceIOStats
	Errors btrfs.DeviceErrors
}

type DeviceStats struct {
	Devices    []PerDeviceStats
	Filesystem btrfs.FilesystemUsage
}

// readDeviceIOStats reads /sys/block/<dev>/stat and returns IO counters.
// See https://www.kernel.org/doc/Documentation/block/stat.txt
func readDeviceIOStats(device string) (*DeviceIOStats, error) {
	resolved := device
	if r, err := filepath.EvalSymlinks(device); err == nil {
		resolved = r
	}
	data, err := os.ReadFile(filepath.Join("/sys/block", filepath.Base(resolved), "stat"))
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
		s.updateDeviceIO(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.updateDeviceIO(ctx)
			}
		}
	}()
}

func (s *Storage) updateDeviceIO(ctx context.Context) {
	devices, err := s.btrfs.Devices(ctx, s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device io updater: failed to discover devices")
		return
	}
	for _, device := range devices {
		io, err := readDeviceIOStats(device)
		if err != nil {
			log.Warn().Err(err).Str("device", device).Msg("device io updater: failed to read IO stats")
			continue
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
	errs, err := s.btrfs.DeviceErrors(ctx, s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device stats updater: btrfs device errors failed")
	} else {
		for _, de := range errs {
			DeviceReadErrsTotal.WithLabelValues(de.Device).Set(float64(de.ReadErrs))
			DeviceWriteErrsTotal.WithLabelValues(de.Device).Set(float64(de.WriteErrs))
			DeviceFlushErrsTotal.WithLabelValues(de.Device).Set(float64(de.FlushErrs))
			DeviceCorruptionErrsTotal.WithLabelValues(de.Device).Set(float64(de.CorruptionErrs))
			DeviceGenerationErrsTotal.WithLabelValues(de.Device).Set(float64(de.GenerationErrs))
		}
	}

	fu, err := s.btrfs.FilesystemUsage(ctx, s.basePath)
	if err != nil {
		log.Warn().Err(err).Msg("device stats updater: btrfs filesystem usage failed")
	} else {
		FilesystemSizeBytes.WithLabelValues(s.basePath).Set(float64(fu.TotalBytes))
		FilesystemUsedBytes.WithLabelValues(s.basePath).Set(float64(fu.UsedBytes))
		FilesystemUnallocatedBytes.WithLabelValues(s.basePath).Set(float64(fu.UnallocatedBytes))
		FilesystemMetadataUsedBytes.WithLabelValues(s.basePath).Set(float64(fu.MetadataUsedBytes))
		FilesystemMetadataTotalBytes.WithLabelValues(s.basePath).Set(float64(fu.MetadataTotalBytes))
		FilesystemDataRatio.WithLabelValues(s.basePath).Set(fu.DataRatio)
	}

	log.Debug().Msg("device stats updater: metrics updated")
}

// DeviceStats collects per-device IO and error stats plus global filesystem usage.
func (s *Storage) DeviceStats(ctx context.Context) (*DeviceStats, error) {
	devList, err := s.btrfs.Devices(ctx, s.basePath)
	if err != nil {
		return nil, fmt.Errorf("discover devices: %w", err)
	}

	errList, err := s.btrfs.DeviceErrors(ctx, s.basePath)
	if err != nil {
		return nil, fmt.Errorf("btrfs device errors: %w", err)
	}
	errByDevice := make(map[string]btrfs.DeviceErrors, len(errList))
	for _, de := range errList {
		errByDevice[de.Device] = de
	}

	var devices []PerDeviceStats
	for _, device := range devList {
		io, err := readDeviceIOStats(device)
		if err != nil {
			log.Warn().Err(err).Str("device", device).Msg("device stats: failed to read IO stats")
			continue
		}
		devices = append(devices, PerDeviceStats{
			Device: device,
			IO:     *io,
			Errors: errByDevice[device],
		})
	}

	fu, err := s.btrfs.FilesystemUsage(ctx, s.basePath)
	if err != nil {
		return nil, fmt.Errorf("btrfs filesystem usage: %w", err)
	}

	return &DeviceStats{
		Devices:    devices,
		Filesystem: fu,
	}, nil
}
