package storage

import (
	"fmt"
	"os"
	"regexp"
)

// --- Error types ---

const (
	ErrInvalid       = "INVALID"
	ErrNotFound      = "NOT_FOUND"
	ErrAlreadyExists = "ALREADY_EXISTS"
	ErrBusy          = "BUSY"
)

type StorageError struct {
	Code    string
	Message string
}

func (e *StorageError) Error() string { return e.Message }

// --- Validation ---

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func validateName(name string) error {
	if !validName.MatchString(name) {
		return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid name: %q (must be 1-128 chars, only a-z A-Z 0-9 _ -)", name)}
	}
	return nil
}

// --- File mode ---

// fileMode converts a traditional Unix octal mode (e.g. 0o2750) to an os.FileMode.
// Go's os.FileMode uses its own bit layout for setuid/setgid/sticky, so passing
// a raw Unix octal value like os.FileMode(0o2770) silently drops the special bits.
// See https://pkg.go.dev/os#FileMode and https://github.com/golang/go/issues/44575.
func fileMode(unixMode uint64) os.FileMode {
	m := os.FileMode(unixMode & 0o777)
	if unixMode&0o4000 != 0 {
		m |= os.ModeSetuid
	}
	if unixMode&0o2000 != 0 {
		m |= os.ModeSetgid
	}
	if unixMode&0o1000 != 0 {
		m |= os.ModeSticky
	}
	return m
}

func unixMode(m os.FileMode) uint64 {
	mode := uint64(m.Perm())
	if m&os.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if m&os.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		mode |= 0o1000
	}
	return mode
}
