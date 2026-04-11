package storage

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
)

// --- Error types ---

const (
	ErrBadRequest    = "BAD_REQUEST"
	ErrUnauthorized  = "UNAUTHORIZED"
	ErrInvalid       = "INVALID"
	ErrNotFound      = "NOT_FOUND"
	ErrAlreadyExists = "ALREADY_EXISTS"
	ErrBusy          = "BUSY"
	ErrMetadata      = "METADATA_ERROR"
	ErrInternal      = "INTERNAL_ERROR"
)

type StorageError struct {
	Code    string
	Message string
}

func (e *StorageError) Error() string { return e.Message }

func requireImmutableLabels(keys []string, labels map[string]string) error {
	for _, k := range keys {
		if labels[k] == "" {
			return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("label %q is required", k)}
		}
	}
	return nil
}

func protectImmutableLabels(keys []string, cur, updated map[string]string) error {
	// Protect explicitly configured immutable keys and soft-reserved keys
	// (clone.source.*, created-by). Soft-reserved labels are set automatically
	// on create and must not be changed or added via update.
	allKeys := make(map[string]struct{}, len(keys)+len(config.SoftReservedLabelKeys))
	for _, k := range keys {
		allKeys[k] = struct{}{}
	}
	for _, k := range config.SoftReservedLabelKeys {
		allKeys[k] = struct{}{}
	}
	for k := range allKeys {
		if v, ok := updated[k]; ok && v != cur[k] {
			// Allow initial set of created-by on volumes migrated from older versions.
			// TODO: remove in future release once all pre-0.10.0 volumes have been backfilled.
			if k == config.LabelCreatedBy && cur[k] == "" {
				continue
			}
			return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("label %q cannot be changed", k)}
		}
		if v := cur[k]; v != "" {
			updated[k] = v
		}
	}
	return nil
}

// validateClientIP ensures client is a valid IPv4 or IPv6 address.
// Rejects wildcards, hostnames, CIDRs, and strings with unsafe characters.
func validateClientIP(client string) error {
	if net.ParseIP(client) == nil {
		return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid client IP: %q (must be a valid IPv4 or IPv6 address)", client)}
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
	for k := range strings.SplitSeq(extra, ",") {
		k = strings.TrimSpace(k)
		if k != "" && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return keys
}
