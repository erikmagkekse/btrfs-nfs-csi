package utils

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
		{"1Ti", 1099511627776},
		{"1Pi", 1125899906842624},
		{"1Ei", 1152921504606846976},
		{"1K", 1000},
		{"1M", 1000000},
		{"1G", 1000000000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSize(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSizeErrors(t *testing.T) {
	tests := []string{"", "abc", "-1Gi", "1.5Gi"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSize(input)
			assert.Error(t, err)
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0B"},
		{1, "1B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1Ki"},
		{1536, "2Ki"},
		{1048576, "1Mi"},
		{1073741824, "1.0Gi"},
		{1610612736, "1.5Gi"},
		{10737418240, "10.0Gi"},
		{1099511627776, "1.0Ti"},
		{5497558138880, "5.0Ti"},
		{1125899906842624, "1.0Pi"},
		{1152921504606846976, "1.0Ei"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatBytes(tt.input))
		})
	}
}
