package utils

import (
	"fmt"
	"strconv"
)

// FormatBytes formats a byte count as a human-readable string (GiB/MiB/KiB).
func FormatBytes(b uint64) string {
	switch {
	case b >= 1<<60:
		return fmt.Sprintf("%.1fEi", float64(b)/float64(uint64(1)<<60))
	case b >= 1<<50:
		return fmt.Sprintf("%.1fPi", float64(b)/float64(uint64(1)<<50))
	case b >= 1<<40:
		return fmt.Sprintf("%.1fTi", float64(b)/float64(uint64(1)<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGi", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0fMi", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0fKi", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// ParseSize parses a human-readable size string (e.g. "10Gi", "500Mi", "1G") to bytes.
func ParseSize(s string) (uint64, error) {
	if len(s) < 2 {
		return strconv.ParseUint(s, 10, 64)
	}
	suffix := s[len(s)-2:]
	num := s[:len(s)-2]
	var multiplier uint64
	switch suffix {
	case "Ei":
		multiplier = uint64(1) << 60
	case "Pi":
		multiplier = uint64(1) << 50
	case "Ti":
		multiplier = uint64(1) << 40
	case "Gi":
		multiplier = 1 << 30
	case "Mi":
		multiplier = 1 << 20
	case "Ki":
		multiplier = 1 << 10
	default:
		suffix = s[len(s)-1:]
		num = s[:len(s)-1]
		switch suffix {
		case "G":
			multiplier = 1_000_000_000
		case "M":
			multiplier = 1_000_000
		case "K":
			multiplier = 1_000
		default:
			return strconv.ParseUint(s, 10, 64)
		}
	}
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return n * multiplier, nil
}
