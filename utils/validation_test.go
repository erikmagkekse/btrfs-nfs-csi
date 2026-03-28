package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidCompression(t *testing.T) {
	valid := []string{"", "none", "zstd", "lzo", "zlib", "zstd:1", "zstd:15", "zlib:9"}
	for _, s := range valid {
		assert.True(t, IsValidCompression(s), "IsValidCompression(%q) should be true", s)
	}

	invalid := []string{"doesnotexist", "zstd:0", "zstd:16", "zstd:420", "zstd:abc", "lz4", "gzip"}
	for _, s := range invalid {
		assert.False(t, IsValidCompression(s), "IsValidCompression(%q) should be false", s)
	}
}
