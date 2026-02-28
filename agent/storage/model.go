package storage

import "time"

// Persisted metadata types

type VolumeMetadata struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	SizeBytes    uint64     `json:"size_bytes"`
	NoCOW        bool       `json:"nocow"`
	Compression  string     `json:"compression"`
	QuotaBytes   uint64     `json:"quota_bytes"`
	UsedBytes    uint64     `json:"used_bytes"`
	UID          int        `json:"uid"`
	GID          int        `json:"gid"`
	Mode         string     `json:"mode"`
	Clients      []string   `json:"clients,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastAttachAt *time.Time `json:"last_attach_at,omitempty"`
}

type SnapshotMetadata struct {
	Name           string    `json:"name"`
	Volume         string    `json:"volume"`
	Path           string    `json:"path"`
	SizeBytes      uint64    `json:"size_bytes"`
	UsedBytes      uint64    `json:"used_bytes"`
	ExclusiveBytes uint64    `json:"exclusive_bytes"`
	ReadOnly       bool      `json:"readonly"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CloneMetadata struct {
	Name           string    `json:"name"`
	SourceSnapshot string    `json:"source_snapshot"`
	Path           string    `json:"path"`
	CreatedAt      time.Time `json:"created_at"`
}

// Request types

type VolumeCreateRequest struct {
	Name        string `json:"name"`
	SizeBytes   uint64 `json:"size_bytes"`
	NoCOW       bool   `json:"nocow"`
	Compression string `json:"compression"`
	QuotaBytes  uint64 `json:"quota_bytes"`
	UID         int    `json:"uid"`
	GID         int    `json:"gid"`
	Mode        string `json:"mode"`
}

type VolumeUpdateRequest struct {
	SizeBytes   *uint64 `json:"size_bytes,omitempty"`
	NoCOW       *bool   `json:"nocow,omitempty"`
	Compression *string `json:"compression,omitempty"`
	UID         *int    `json:"uid,omitempty"`
	GID         *int    `json:"gid,omitempty"`
	Mode        *string `json:"mode,omitempty"`
}

type SnapshotCreateRequest struct {
	Volume string `json:"volume"`
	Name   string `json:"name"`
}

type CloneCreateRequest struct {
	Snapshot string `json:"snapshot"`
	Name     string `json:"name"`
}

type ExportEntry struct {
	Path   string `json:"path"`
	Client string `json:"client"`
}
