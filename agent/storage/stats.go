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

// DeviceState represents the current state of a block device in the filesystem.
type DeviceState struct {
	btrfs.BTRFSDevice
	IO DeviceIOStats
}

type DeviceStats struct {
	Devices    []DeviceState
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
func (s *Storage) startDeviceIOUpdater(ctx context.Context, interval time.Duration) {
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
	// 1. fetch from kernel
	devices, err := s.btrfs.Devices(ctx, s.mountPoint)
	if err != nil {
		log.Warn().Err(err).Msg("device io updater: failed to discover devices")
		return
	}

	ioByDevice := make(map[string]*DeviceIOStats, len(devices))
	for _, d := range devices {
		if d.Missing {
			continue
		}
		io, err := readDeviceIOStats(d.Device)
		if err != nil {
			log.Warn().Err(err).Str("device", d.Device).Msg("device io updater: failed to read IO stats")
			continue
		}
		ioByDevice[d.Device] = io
	}

	// 2. load cache, merge, store
	prev := s.cachedDevices.Load()
	prevByDevID := make(map[string]DeviceState)
	if prev != nil {
		for _, d := range *prev {
			prevByDevID[d.DevID] = d
		}
	}

	var states []DeviceState
	currentDevIDs := make(map[string]struct{}, len(devices))
	for _, d := range devices {
		currentDevIDs[d.DevID] = struct{}{}
		ds := DeviceState{
			BTRFSDevice: btrfs.BTRFSDevice{
				DevID: d.DevID, Device: d.Device, Missing: d.Missing,
				SizeBytes: d.SizeBytes, AllocatedBytes: d.AllocatedBytes,
				Errors: prevByDevID[d.DevID].Errors,
			},
		}
		if io, ok := ioByDevice[d.Device]; ok {
			ds.IO = *io
		}

		// log state transitions
		prev, known := prevByDevID[d.DevID]
		if !known {
			log.Info().Str("devid", d.DevID).Str("device", d.Device).Bool("missing", d.Missing).Msg("device io updater: new device discovered")
		} else if d.Missing && !prev.Missing {
			log.Warn().Str("devid", d.DevID).Str("device", d.Device).Msg("device io updater: device went missing")
		} else if !d.Missing && prev.Missing {
			log.Info().Str("devid", d.DevID).Str("device", d.Device).Msg("device io updater: device recovered")
		}

		states = append(states, ds)
	}
	for devID, prev := range prevByDevID {
		if _, ok := currentDevIDs[devID]; !ok {
			log.Info().Str("devid", devID).Str("device", prev.Device).Msg("device io updater: device removed")
		}
	}
	s.cachedDevices.Store(&states)

	// 3. update prometheus metrics
	DeviceSizeBytes.Reset()
	DeviceAllocatedBytes.Reset()
	DevicePresentGauge.Reset()
	DeviceReadBytesTotal.Reset()
	DeviceReadIOsTotal.Reset()
	DeviceReadTimeSecondsTotal.Reset()
	DeviceWriteBytesTotal.Reset()
	DeviceWriteIOsTotal.Reset()
	DeviceWriteTimeSecondsTotal.Reset()
	DeviceIOsInProgress.Reset()
	DeviceIOTimeSecondsTotal.Reset()
	DeviceIOWeightedTimeSecondsTotal.Reset()

	for _, ds := range states {
		DeviceSizeBytes.WithLabelValues(ds.Device).Set(float64(ds.SizeBytes))
		DeviceAllocatedBytes.WithLabelValues(ds.Device).Set(float64(ds.AllocatedBytes))
		if ds.Missing {
			DevicePresentGauge.WithLabelValues(ds.Device).Set(0)
			continue
		}
		DevicePresentGauge.WithLabelValues(ds.Device).Set(1)
		DeviceReadBytesTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.ReadBytes))
		DeviceReadIOsTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.ReadIOs))
		DeviceReadTimeSecondsTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.ReadTimeMs) / 1000.0)
		DeviceWriteBytesTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.WriteBytes))
		DeviceWriteIOsTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.WriteIOs))
		DeviceWriteTimeSecondsTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.WriteTimeMs) / 1000.0)
		DeviceIOsInProgress.WithLabelValues(ds.Device).Set(float64(ds.IO.IOsInProgress))
		DeviceIOTimeSecondsTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.IOTimeMs) / 1000.0)
		DeviceIOWeightedTimeSecondsTotal.WithLabelValues(ds.Device).Set(float64(ds.IO.WeightedIOTimeMs) / 1000.0)
	}
}

// StartDeviceStatsUpdater polls btrfs device errors and filesystem usage (default 1m).
func (s *Storage) startDeviceStatsUpdater(ctx context.Context, interval time.Duration) {
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
	// 1. fetch from kernel
	errs, errErr := s.btrfs.DeviceErrors(ctx, s.basePath)
	fu, fuErr := s.btrfs.FilesystemUsage(ctx, s.mountPoint)

	// 2. load cache, merge, store (only on success, preserve previous values on error)
	if errErr == nil {
		errByDevice := make(map[string]btrfs.DeviceErrors, len(errs))
		for _, de := range errs {
			errByDevice[de.Device] = de
		}
		if prev := s.cachedDevices.Load(); prev != nil {
			states := make([]DeviceState, len(*prev))
			for i, d := range *prev {
				states[i] = d
				states[i].Errors = errByDevice[d.Device]
			}
			s.cachedDevices.Store(&states)
		}
	}
	if fuErr == nil {
		s.cachedFilesystem.Store(&fu)
	}

	// 3. update prometheus metrics
	if errErr != nil {
		log.Warn().Err(errErr).Msg("device stats updater: btrfs device errors failed")
	} else {
		DeviceReadErrsTotal.Reset()
		DeviceWriteErrsTotal.Reset()
		DeviceFlushErrsTotal.Reset()
		DeviceCorruptionErrsTotal.Reset()
		DeviceGenerationErrsTotal.Reset()
		for _, de := range errs {
			DeviceReadErrsTotal.WithLabelValues(de.Device).Set(float64(de.ReadErrs))
			DeviceWriteErrsTotal.WithLabelValues(de.Device).Set(float64(de.WriteErrs))
			DeviceFlushErrsTotal.WithLabelValues(de.Device).Set(float64(de.FlushErrs))
			DeviceCorruptionErrsTotal.WithLabelValues(de.Device).Set(float64(de.CorruptionErrs))
			DeviceGenerationErrsTotal.WithLabelValues(de.Device).Set(float64(de.GenerationErrs))
		}
	}

	if fuErr != nil {
		log.Warn().Err(fuErr).Msg("device stats updater: btrfs filesystem usage failed")
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

// IsDegraded returns true if any device is missing or has btrfs errors.
func (s *Storage) IsDegraded() bool {
	devs := s.cachedDevices.Load()
	if devs == nil {
		return false
	}
	for _, d := range *devs {
		if d.Missing {
			return true
		}
		if d.HasErrors() {
			return true
		}
	}
	return false
}

// DeviceStats returns cached per-device IO/error stats and filesystem usage.
// IO stats are updated by the IO poller (5s), errors and filesystem by the stats poller (1m).
func (s *Storage) DeviceStats(ctx context.Context) (*DeviceStats, error) {
	devs := s.cachedDevices.Load()
	if devs == nil {
		return nil, fmt.Errorf("device stats not yet available")
	}
	ds := &DeviceStats{Devices: *devs}
	if fu := s.cachedFilesystem.Load(); fu != nil {
		ds.Filesystem = *fu
	}
	return ds, nil
}
