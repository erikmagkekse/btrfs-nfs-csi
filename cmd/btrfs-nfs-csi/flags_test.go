package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInjectWatchDefault(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		out  []string
	}{
		{"bare -w at end", []string{"volume", "ls", "-w"}, []string{"volume", "ls", "-w", "2s"}},
		{"bare --watch at end", []string{"volume", "ls", "--watch"}, []string{"volume", "ls", "--watch", "2s"}},
		{"-w with value", []string{"volume", "ls", "-w", "500ms"}, []string{"volume", "ls", "-w", "500ms"}},
		{"-w followed by flag", []string{"volume", "ls", "-w", "-l", "env=prod"}, []string{"volume", "ls", "-w", "2s", "-l", "env=prod"}},
		{"no -w", []string{"volume", "ls"}, []string{"volume", "ls"}},
		{"empty", []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.out, injectWatchDefault(tt.in))
		})
	}
}

func TestEmptyHint_NoOutput(t *testing.T) {
	// count > 0 should not print
	emptyHint("volumes", 1, false, false)
}

func TestFmtDuration(t *testing.T) {
	assert.Equal(t, "0ms", fmtDuration(0))
	assert.Equal(t, "50ms", fmtDuration(50_000_000))
	assert.Equal(t, "1s", fmtDuration(1_000_000_000))
	assert.Equal(t, "10s", fmtDuration(10_000_000_000))
}
