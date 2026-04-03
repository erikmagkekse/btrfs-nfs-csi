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
