package utils

import (
	"strconv"
	"strings"
)

var validCompressionAlgo = map[string]bool{"zstd": true, "lzo": true, "zlib": true}

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
