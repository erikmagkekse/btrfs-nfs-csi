package v1

import (
	"encoding/json"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
)

// Type aliases - canonical definitions live in the storage package,
// re-exported here for backward compatibility (client, controller).
type (
	VolumeCreateRequest   = storage.VolumeCreateRequest
	VolumeUpdateRequest   = storage.VolumeUpdateRequest
	SnapshotCreateRequest = storage.SnapshotCreateRequest
	CloneCreateRequest    = storage.CloneCreateRequest
	VolumeCloneRequest    = storage.VolumeCloneRequest
	VolumeMetadata        = storage.VolumeMetadata
	SnapshotMetadata      = storage.SnapshotMetadata
	CloneMetadata         = storage.CloneMetadata
	ExportEntry           = storage.ExportEntry
)

const (
	HealthStatusOK       = "ok"
	HealthStatusDegraded = "degraded"

	TaskStatusPending   = string(task.TaskPending)
	TaskStatusRunning   = string(task.TaskRunning)
	TaskStatusCompleted = string(task.TaskCompleted)
	TaskStatusFailed    = string(task.TaskFailed)
	TaskStatusCancelled = string(task.TaskCancelled)

	TaskTypeScrub = string(task.TypeScrub)
)

// request models (HTTP-layer only)

type ExportRequest struct {
	Client string `json:"client"`
}

// response models

type VolumeResponse struct {
	Name      string    `json:"name"`
	SizeBytes uint64    `json:"size_bytes"`
	UsedBytes uint64    `json:"used_bytes"`
	Clients   int       `json:"clients"`
	CreatedAt time.Time `json:"created_at"`
}

type VolumeDetailResponse struct {
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
	Clients      []string   `json:"clients"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastAttachAt *time.Time `json:"last_attach_at,omitempty"`
}

type VolumeListResponse struct {
	Volumes []VolumeResponse `json:"volumes"`
	Total   int              `json:"total"`
}

type VolumeDetailListResponse struct {
	Volumes []VolumeDetailResponse `json:"volumes"`
	Total   int                    `json:"total"`
}

type SnapshotResponse struct {
	Name      string    `json:"name"`
	Volume    string    `json:"volume"`
	SizeBytes uint64    `json:"size_bytes"`
	UsedBytes uint64    `json:"used_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type SnapshotDetailResponse struct {
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
	Total     int                `json:"total"`
}

type SnapshotDetailListResponse struct {
	Snapshots []SnapshotDetailResponse `json:"snapshots"`
	Total     int                      `json:"total"`
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
	Statfs StatfsResponse          `json:"statfs"`
	Btrfs  FilesystemStatsResponse `json:"btrfs"`
}

type StatfsResponse struct {
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

type DeviceStatsResponse struct {
	DevID          string                `json:"devid"`
	Device         string                `json:"device"`
	Missing        bool                  `json:"missing"`
	SizeBytes      uint64                `json:"size_bytes"`
	AllocatedBytes uint64                `json:"allocated_bytes"`
	IO             DeviceIOStatsResponse `json:"io"`
	Errors         DeviceErrorsResponse  `json:"errors"`
}

type HealthResponse struct {
	Status        string            `json:"status"`
	Version       string            `json:"version"`
	Commit        string            `json:"commit"`
	UptimeSeconds int               `json:"uptime_seconds"`
	Features      map[string]string `json:"features"`
}

type DeviceIOStatsResponse struct {
	ReadBytesTotal        uint64 `json:"read_bytes_total"`
	ReadIOsTotal          uint64 `json:"read_ios_total"`
	ReadTimeMsTotal       uint64 `json:"read_time_ms_total"`
	WriteBytesTotal       uint64 `json:"write_bytes_total"`
	WriteIOsTotal         uint64 `json:"write_ios_total"`
	WriteTimeMsTotal      uint64 `json:"write_time_ms_total"`
	IOsInProgress         uint64 `json:"ios_in_progress"`
	IOTimeMsTotal         uint64 `json:"io_time_ms_total"`
	WeightedIOTimeMsTotal uint64 `json:"weighted_io_time_ms_total"`
}

type DeviceErrorsResponse struct {
	ReadErrs       uint64 `json:"read_errs"`
	WriteErrs      uint64 `json:"write_errs"`
	FlushErrs      uint64 `json:"flush_errs"`
	CorruptionErrs uint64 `json:"corruption_errs"`
	GenerationErrs uint64 `json:"generation_errs"`
}

type FilesystemStatsResponse struct {
	TotalBytes         uint64                `json:"total_bytes"`
	UsedBytes          uint64                `json:"used_bytes"`
	FreeBytes          uint64                `json:"free_bytes"`
	UnallocatedBytes   uint64                `json:"unallocated_bytes"`
	MetadataUsedBytes  uint64                `json:"metadata_used_bytes"`
	MetadataTotalBytes uint64                `json:"metadata_total_bytes"`
	DataRatio          float64               `json:"data_ratio"`
	Devices            []DeviceStatsResponse `json:"devices"`
}

type TaskStartResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type TaskResponse struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	Progress    int        `json:"progress"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type TaskDetailResponse struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Status      string          `json:"status"`
	Progress    int             `json:"progress"`
	Result      json.RawMessage `json:"result,omitempty"`
	Info        string          `json:"info,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type TaskListResponse struct {
	Tasks []TaskResponse `json:"tasks"`
	Total int            `json:"total"`
}

type TaskDetailListResponse struct {
	Tasks []TaskDetailResponse `json:"tasks"`
	Total int                  `json:"total"`
}

type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
