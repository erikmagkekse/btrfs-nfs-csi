package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// withStdin pipes s into os.Stdin for the duration of fn. Used to feed
// hash-token from a test rather than the terminal.
func withStdin(t *testing.T, s string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	go func() {
		_, _ = io.WriteString(w, s)
		_ = w.Close()
	}()
	fn()
}

func TestHashToken_RoundTrips(t *testing.T) {
	cmd := hashTokenCmd()
	require.NoError(t, cmd.Set("cost", "4")) // MinCost keeps the test fast

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origOut := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origOut })

	withStdin(t, "s3cret\n", func() {
		require.NoError(t, cmd.Action(context.Background(), cmd))
	})
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	hash := strings.TrimSpace(string(out))
	assert.True(t, strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$"),
		"hash should start with a bcrypt PHC marker, got %q", hash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("s3cret")), "hash should verify against original token")
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong")), "hash should reject wrong token")
}

func TestHashToken_RejectsEmpty(t *testing.T) {
	cmd := hashTokenCmd()
	require.NoError(t, cmd.Set("cost", "4"))

	withStdin(t, "\n", func() {
		err := cmd.Action(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})
}

func TestHashToken_RejectsOutOfRangeCost(t *testing.T) {
	cmd := hashTokenCmd()
	require.NoError(t, cmd.Set("cost", "2")) // below bcrypt.MinCost (4)

	withStdin(t, "anything\n", func() {
		err := cmd.Action(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of range")
	})
}
