package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// compressionMaxLevel maps each algorithm to its maximum allowed level.
// A max of 0 means no level suffix is permitted.
var compressionMaxLevel = map[string]int{"zstd": 15, "zlib": 9, "lzo": 0}

// MaxID is the maximum valid UID/GID value (nobody).
// TODO: consider supporting supplementary IDs above this limit
const MaxID = 65534

func ValidateUID(uid int) error {
	if uid < 0 || uid > MaxID {
		return fmt.Errorf("invalid uid %d: must be between 0 and %d", uid, MaxID)
	}
	return nil
}

func ValidateGID(gid int) error {
	if gid < 0 || gid > MaxID {
		return fmt.Errorf("invalid gid %d: must be between 0 and %d", gid, MaxID)
	}
	return nil
}

// ValidateMode parses an octal mode string and validates the range (0-7777).
func ValidateMode(s string) (uint64, error) {
	mode, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q: %v", s, err)
	}
	if mode > 0o7777 {
		return 0, fmt.Errorf("invalid mode %q: must not exceed 7777 (octal)", s)
	}
	return mode, nil
}

func IsValidCompression(s string) bool {
	if s == "" || s == "none" {
		return true
	}
	parts := strings.SplitN(s, ":", 2)
	maxLevel, ok := compressionMaxLevel[parts[0]]
	if !ok {
		return false
	}
	if len(parts) == 2 {
		if maxLevel == 0 {
			return false
		}
		level, err := strconv.Atoi(parts[1])
		if err != nil || level < 1 || level > maxLevel {
			return false
		}
	}
	return true
}
