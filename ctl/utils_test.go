package ctl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"0", 0},
		{"1024", 1024},
		{"1Ki", 1024},
		{"10Ki", 10240},
		{"1Mi", 1048576},
		{"1Gi", 1073741824},
		{"5Gi", 5368709120},
		{"1K", 1000},
		{"1M", 1000000},
		{"1G", 1000000000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSize(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSizeErrors(t *testing.T) {
	tests := []string{
		"",
		"abc",
		"-1Gi",
		"1.5Gi",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseSize(input)
			assert.Error(t, err)
		})
	}
}

func TestUsedPct(t *testing.T) {
	assert.Equal(t, 0.0, usedPct(0, 0))
	assert.Equal(t, 0.0, usedPct(0, 100))
	assert.Equal(t, 50.0, usedPct(50, 100))
	assert.Equal(t, 100.0, usedPct(100, 100))
	assert.InDelta(t, 33.3, usedPct(1, 3), 0.1)
}

func TestWrapErr(t *testing.T) {
	// Non-API error passes through
	err := wrapErr(assert.AnError, "volume", "test")
	assert.Equal(t, assert.AnError, err)

	// Nil returns nil
	assert.Nil(t, wrapErr(nil, "volume", "test"))
}
