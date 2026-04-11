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
	for line := range strings.SplitSeq(out, "\n") {
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
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format: [/dev/sda].write_io_errs    0
		devicePart, rest, ok := strings.Cut(line, "].")
		if !ok {
			continue
		}
		device := strings.TrimPrefix(devicePart, "[")
		if cur == nil || cur.Device != device {
			all = append(all, DeviceErrors{Device: device})
			cur = &all[len(all)-1]
		}
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

	for line := range strings.SplitSeq(out, "\n") {
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
				if _, val, ok := strings.Cut(trimmed, ":"); ok {
					if v, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
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
	k, rawVal, found := strings.Cut(line, ":")
	if !found {
		return "", 0, false
	}
	key = strings.TrimSpace(k)
	raw := strings.TrimSpace(rawVal)
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
	for line := range strings.SplitSeq(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)

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

// subvolEntry holds a parsed line from `btrfs subvolume list -o`.
type subvolEntry struct {
	ID   string
	Path string
}

// parseSubvolumeListFull parses `btrfs subvolume list -o` output into ID + path pairs.
// Format: ID 259 gen 12 top level 5 path tenant/vol1/data
func parseSubvolumeListFull(out string) []subvolEntry {
	var entries []subvolEntry
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 || fields[0] != "ID" {
			continue
		}
		_, path, ok := strings.Cut(line, " path ")
		if !ok {
			continue
		}
		entries = append(entries, subvolEntry{
			ID:   fields[1],
			Path: path,
		})
	}
	return entries
}

// parseQgroupMap parses `btrfs qgroup show -re --raw` output into a map keyed by qgroup ID.
// Format:
//
//	qgroupid         rfer         excl
//	--------         ----         ----
//	0/259        16384         8192
func parseQgroupMap(out string) (map[string]QgroupInfo, error) {
	result := make(map[string]QgroupInfo)
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.Contains(fields[0], "/") {
			continue
		}
		var info QgroupInfo
		var err error
		if info.Referenced, err = strconv.ParseUint(fields[1], 10, 64); err != nil {
			return nil, fmt.Errorf("parse referenced bytes %q: %w", fields[1], err)
		}
		if info.Exclusive, err = strconv.ParseUint(fields[2], 10, 64); err != nil {
			return nil, fmt.Errorf("parse exclusive bytes %q: %w", fields[2], err)
		}
		result[fields[0]] = info
	}
	return result, nil
}

// parseProfileSizeUsed parses "Metadata,DUP: Size:1073741824, Used:536870912".
func parseProfileSizeUsed(line string) (size, used uint64) {
	if _, after, ok := strings.Cut(line, "Size:"); ok {
		raw := after
		if end := strings.IndexAny(raw, ", \t"); end > 0 {
			raw = raw[:end]
		}
		v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err == nil {
			size = v
		}
	}
	if _, after, ok := strings.Cut(line, "Used:"); ok {
		raw := after
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
