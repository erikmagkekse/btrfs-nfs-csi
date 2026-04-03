package ctl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestFormatLabels(t *testing.T) {
	assert.Equal(t, "none", formatLabels(nil))
	assert.Equal(t, "none", formatLabels(map[string]string{}))
	assert.Equal(t, "env=prod", formatLabels(map[string]string{"env": "prod"}))
	assert.Equal(t, "env=prod, team=be", formatLabels(map[string]string{"team": "be", "env": "prod"}))
}

func TestFormatLabelsShort(t *testing.T) {
	assert.Equal(t, "-", formatLabelsShort(nil))
	assert.Equal(t, "-", formatLabelsShort(map[string]string{}))

	// created-by pinned first
	assert.Equal(t, "created-by=cli, env=prod", formatLabelsShort(map[string]string{"env": "prod", "created-by": "cli"}))

	// only created-by
	assert.Equal(t, "created-by=csi", formatLabelsShort(map[string]string{"created-by": "csi"}))

	// no created-by
	assert.Equal(t, "env=prod", formatLabelsShort(map[string]string{"env": "prod"}))

	// truncation at 48 chars
	long := map[string]string{"a": "xxxxxxxxxxxxxxxxxx", "b": "yyyyyyyyyyyyyyyyyy", "c": "zzzzzzzzzzzzzzzzzz"}
	result := formatLabelsShort(long)
	assert.LessOrEqual(t, len(result), 48)
	assert.True(t, len(result) >= 3 && result[len(result)-3:] == "...")
}
