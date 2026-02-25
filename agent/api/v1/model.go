package v1

import (
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
)

// Type aliases - canonical definitions live in the storage package,
// re-exported here for backward compatibility (client, controller).
type (
	VolumeCreateRequest   = storage.VolumeCreateRequest
	VolumeUpdateRequest   = storage.VolumeUpdateRequest
	SnapshotCreateRequest = storage.SnapshotCreateRequest
	CloneCreateRequest    = storage.CloneCreateRequest
	VolumeMetadata        = storage.VolumeMetadata
	SnapshotMetadata      = storage.SnapshotMetadata
	CloneMetadata         = storage.CloneMetadata
	ExportEntry           = storage.ExportEntry
)

// request models (HTTP-layer only)

type ExportRequest struct {
	Client string `json:"client"`
}

// response models

type VolumeResponse struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	SizeBytes    uint64    `json:"size_bytes"`
	NoCOW        bool      `json:"nocow"`
	Compression  string    `json:"compression"`
	QuotaBytes   uint64    `json:"quota_bytes"`
	UsedBytes    uint64    `json:"used_bytes"`
	UID          int       `json:"uid"`
	GID          int       `json:"gid"`
	Mode         string    `json:"mode"`
	Clients      int       `json:"clients"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastAttachAt *time.Time `json:"last_attach_at,omitempty"`
}

type VolumeListResponse struct {
	Volumes []VolumeResponse `json:"volumes"`
}

type SnapshotResponse struct {
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

type SnapshotListResponse struct {
	Snapshots []SnapshotResponse `json:"snapshots"`
}

type CloneResponse struct {
	Name           string    `json:"name"`
	SourceSnapshot string    `json:"source_snapshot"`
	Path           string    `json:"path"`
	CreatedAt      time.Time `json:"created_at"`
}

type ExportListResponse struct {
	Exports []ExportEntry `json:"exports"`
}

type StatsResponse struct {
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

type HealthResponse struct {
	Status        string            `json:"status"`
	Version       string            `json:"version"`
	Commit        string            `json:"commit"`
	UptimeSeconds int               `json:"uptime_seconds"`
	Features      map[string]string `json:"features"`
}

type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
