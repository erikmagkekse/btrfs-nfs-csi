package storage

import (
	"fmt"
	"os"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
)

// --- Error types ---

const (
	ErrInvalid       = "INVALID"
	ErrNotFound      = "NOT_FOUND"
	ErrAlreadyExists = "ALREADY_EXISTS"
	ErrBusy          = "BUSY"
	ErrMetadata      = "METADATA_ERROR"
)


type StorageError struct {
	Code    string
	Message string
}

func (e *StorageError) Error() string { return e.Message }

// --- Validation ---

func validateName(name string) error {
	if !config.ValidName.MatchString(name) {
		return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid name: %q (must be 1-128 chars, only a-z A-Z 0-9 _ -)", name)}
	}
	return nil
}

func validateLabels(labels map[string]string) error {
	if len(labels) > config.MaxLabels {
		return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("too many labels: %d (max %d)", len(labels), config.MaxLabels)}
	}
	for k, v := range labels {
		if !config.ValidLabelKey.MatchString(k) {
			return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid label key: %q", k)}
		}
		if !config.ValidLabelVal.MatchString(v) {
			return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid label value: %q", v)}
		}
	}
	return nil
}

func requireImmutableLabels(keys []string, labels map[string]string) error {
	for _, k := range keys {
		if labels[k] == "" {
			return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("label %q is required", k)}
		}
	}
	return nil
}

func protectImmutableLabels(keys []string, cur, updated map[string]string) error {
	for _, k := range keys {
		if v, ok := updated[k]; ok && v != cur[k] {
			return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("label %q cannot be changed", k)}
		}
		if v := cur[k]; v != "" {
			updated[k] = v
		}
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

var defaultImmutableLabelKeys = []string{config.LabelCreatedBy}

func ImmutableLabelKeys(extra string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range defaultImmutableLabelKeys {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	for _, k := range strings.Split(extra, ",") {
		k = strings.TrimSpace(k)
		if k != "" && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return keys
}
