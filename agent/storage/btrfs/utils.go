package btrfs

import (
	"fmt"
	"strconv"
	"strings"
)

// parseDevices extracts device info from `btrfs filesystem show --raw` output.
// Format: devid    1 size 10737418240 used 1354235904 path /dev/sda
// Missing (known path): devid    2 size 0 used 0 path /dev/sdc MISSING
// Missing (never seen): devid    2 size 0 used 0 path <missing disk> MISSING
// Missing (empty path): devid    2 size 0 used 0 path  MISSING
func parseDevices(out string) ([]BTRFSDevice, error) {
	var devices []BTRFSDevice
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "devid") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		devID := fields[1]

		idx := strings.LastIndex(line, " path ")
		if idx < 0 {
			continue
		}
		pathPart := strings.TrimSpace(line[idx+6:])
		missing := strings.HasSuffix(pathPart, "MISSING")
		path := pathPart
		if missing {
			path = strings.TrimSpace(strings.TrimSuffix(pathPart, "MISSING"))
		}

		// format (--raw): devid    1 size 10737418240 used 1354235904 path /dev/sdb
		var sizeBytes, allocBytes uint64
		if len(fields) >= 8 {
			var sizeErr, allocErr error
			sizeBytes, sizeErr = strconv.ParseUint(fields[3], 10, 64)
			allocBytes, allocErr = strconv.ParseUint(fields[5], 10, 64)
			if !missing && (sizeErr != nil || allocErr != nil) {
				return nil, fmt.Errorf("devid %s: failed to parse size/used fields from %q", devID, line)
			}
		}

		devices = append(devices, BTRFSDevice{
			DevID:          devID,
			Device:         strings.TrimSpace(path),
			Missing:        missing,
			SizeBytes:      sizeBytes,
			AllocatedBytes: allocBytes,
		})
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no devices found in btrfs filesystem show output")
	}
	return devices, nil
}

func parseDeviceErrors(out string) ([]DeviceErrors, error) {
	var all []DeviceErrors
	var cur *DeviceErrors
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format: [/dev/sda].write_io_errs    0
		bracket := strings.Index(line, "].")
		if bracket < 0 {
			continue
		}
		device := line[1:bracket]
		if cur == nil || cur.Device != device {
			all = append(all, DeviceErrors{Device: device})
			cur = &all[len(all)-1]
		}
		rest := line[bracket+2:]
		parts := strings.Fields(rest)
		if len(parts) != 2 {
			continue
		}
		val, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse %q value %q: %w", parts[0], parts[1], err)
		}
		switch parts[0] {
		case "write_io_errs":
			cur.WriteErrs = val
		case "read_io_errs":
			cur.ReadErrs = val
		case "flush_io_errs":
			cur.FlushErrs = val
		case "corruption_errs":
			cur.CorruptionErrs = val
		case "generation_errs":
			cur.GenerationErrs = val
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no device found in btrfs device stats output")
	}
	return all, nil
}

func parseFilesystemUsage(out string) (FilesystemUsage, error) {
	var fu FilesystemUsage
	var inOverall bool

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Overall:") {
			inOverall = true
			continue
		}

		if inOverall {
			if trimmed == "" {
				inOverall = false
				continue
			}
			// "Data ratio:" is a float, not a uint - handle separately
			if strings.HasPrefix(trimmed, "Data ratio:") {
				if parts := strings.SplitN(trimmed, ":", 2); len(parts) == 2 {
					if v, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
						fu.DataRatio = v
					}
				}
				continue
			}
			key, val, ok := parseKVBytes(trimmed)
			if !ok {
				continue
			}
			switch key {
			case "Device size":
				fu.TotalBytes = val
			case "Device unallocated":
				fu.UnallocatedBytes = val
			case "Used":
				fu.UsedBytes = val
			case "Free (estimated)":
				fu.FreeBytes = val
			}
		}

		// Parse Metadata profile line: Metadata,DUP: Size:1073741824, Used:536870912
		if strings.HasPrefix(trimmed, "Metadata,") {
			size, used := parseProfileSizeUsed(trimmed)
			fu.MetadataTotalBytes = size
			fu.MetadataUsedBytes = used
		}
	}
	return fu, nil
}

// parseKVBytes parses "Key: 12345" lines, stripping any parenthetical suffix.
// Uses the first ":" as the separator to handle keys like "Free (estimated)"
// and values like "(min: 12345)".
func parseKVBytes(line string) (key string, val uint64, ok bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	key = strings.TrimSpace(parts[0])
	raw := strings.TrimSpace(parts[1])
	// strip parenthetical like "(min: 12345)"
	if p := strings.Index(raw, "("); p > 0 {
		raw = strings.TrimSpace(raw[:p])
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return key, v, true
}

// parseScrubStatus parses `btrfs scrub status -R` output.
// Key lines: "Status: running/finished/aborted/no stats available"
// and "key: value" pairs for counters.
func parseScrubStatus(out string) (*ScrubStatus, error) {
	s := &ScrubStatus{}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Status":
			s.Running = val == "running"
		case "data_bytes_scrubbed":
			s.DataBytesScrubbed, _ = strconv.ParseUint(val, 10, 64)
		case "tree_bytes_scrubbed":
			s.TreeBytesScrubbed, _ = strconv.ParseUint(val, 10, 64)
		case "read_errors":
			s.ReadErrors, _ = strconv.ParseUint(val, 10, 64)
		case "csum_errors":
			s.CSumErrors, _ = strconv.ParseUint(val, 10, 64)
		case "verify_errors":
			s.VerifyErrors, _ = strconv.ParseUint(val, 10, 64)
		case "super_errors":
			s.SuperErrors, _ = strconv.ParseUint(val, 10, 64)
		case "uncorrectable_errors":
			s.UncorrectableErrs, _ = strconv.ParseUint(val, 10, 64)
		case "corrected_errors":
			s.CorrectedErrs, _ = strconv.ParseUint(val, 10, 64)
		}
	}
	return s, nil
}

// parseProfileSizeUsed parses "Metadata,DUP: Size:1073741824, Used:536870912".
func parseProfileSizeUsed(line string) (size, used uint64) {
	if idx := strings.Index(line, "Size:"); idx >= 0 {
		raw := line[idx+len("Size:"):]
		if end := strings.IndexAny(raw, ", \t"); end > 0 {
			raw = raw[:end]
		}
		v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err == nil {
			size = v
		}
	}
	if idx := strings.Index(line, "Used:"); idx >= 0 {
		raw := line[idx+len("Used:"):]
		if end := strings.IndexAny(raw, ", \t"); end > 0 {
			raw = raw[:end]
		}
		v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err == nil {
			used = v
		}
	}
	return
}
