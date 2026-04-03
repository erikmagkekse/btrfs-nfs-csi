package v1

import (
	"net/http"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"

	"github.com/labstack/echo/v5"
)

type Handler struct {
	Store *storage.Storage
}

// --- Volumes ---

func volumeResponseFrom(meta *storage.VolumeMetadata) VolumeResponse {
	return VolumeResponse{
		Name:      meta.Name,
		SizeBytes: meta.SizeBytes,
		UsedBytes: meta.UsedBytes,
		Clients:   len(meta.Clients),
		CreatedAt: meta.CreatedAt,
	}
}

func (h *Handler) CreateVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.VolumeCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body", Code: "BAD_REQUEST"})
	}

	meta, err := h.Store.CreateVolume(c.Request().Context(), tenant, req)
	if err != nil {
		if meta != nil {
			return c.JSON(http.StatusConflict, volumeDetailResponseFrom(meta))
		}
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, volumeDetailResponseFrom(meta))
}

func (h *Handler) ListVolumes(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	vols, err := h.Store.ListVolumes(tenant)
	if err != nil {
		return StorageError(c, err)
	}

	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		vols = filterByLabels(vols, filters)
	}

	if c.QueryParam("detail") == "true" {
		resp := make([]VolumeDetailResponse, len(vols))
		for i := range vols {
			resp[i] = volumeDetailResponseFrom(&vols[i])
		}
		return c.JSON(http.StatusOK, VolumeDetailListResponse{Volumes: resp, Total: len(resp)})
	}

	resp := make([]VolumeResponse, len(vols))
	for i := range vols {
		resp[i] = volumeResponseFrom(&vols[i])
	}

	return c.JSON(http.StatusOK, VolumeListResponse{Volumes: resp, Total: len(resp)})
}

func volumeDetailResponseFrom(meta *storage.VolumeMetadata) VolumeDetailResponse {
	clients := meta.Clients
	if clients == nil {
		clients = []string{}
	}
	return VolumeDetailResponse{
		Name:         meta.Name,
		Path:         meta.Path,
		SizeBytes:    meta.SizeBytes,
		NoCOW:        meta.NoCOW,
		Compression:  meta.Compression,
		QuotaBytes:   meta.QuotaBytes,
		UsedBytes:    meta.UsedBytes,
		UID:          meta.UID,
		GID:          meta.GID,
		Mode:         meta.Mode,
		Labels:       meta.Labels,
		Clients:      clients,
		CreatedAt:    meta.CreatedAt,
		UpdatedAt:    meta.UpdatedAt,
		LastAttachAt: meta.LastAttachAt,
	}
}

func (h *Handler) GetVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	meta, err := h.Store.GetVolume(tenant, c.Param("name"))
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, volumeDetailResponseFrom(meta))
}

func (h *Handler) UpdateVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req storage.VolumeUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body", Code: "BAD_REQUEST"})
	}

	meta, err := h.Store.UpdateVolume(c.Request().Context(), tenant, name, req)
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, volumeDetailResponseFrom(meta))
}

func (h *Handler) DeleteVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	if err := h.Store.DeleteVolume(c.Request().Context(), tenant, c.Param("name")); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) ExportVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req ExportRequest
	if err := c.Bind(&req); err != nil || req.Client == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "client is required", Code: "BAD_REQUEST"})
	}

	if err := h.Store.ExportVolume(c.Request().Context(), tenant, name, req.Client); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) UnexportVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req ExportRequest
	if err := c.Bind(&req); err != nil || req.Client == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "client is required", Code: "BAD_REQUEST"})
	}

	if err := h.Store.UnexportVolume(c.Request().Context(), tenant, name, req.Client); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) ListExports(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	entries, err := h.Store.ListExports(c.Request().Context(), tenant)
	if err != nil {
		return StorageError(c, err)
	}

	if entries == nil {
		entries = []storage.ExportEntry{}
	}

	return c.JSON(http.StatusOK, ExportListResponse{Exports: entries})
}

func (h *Handler) Stats(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	fs, err := h.Store.Stats(tenant)
	if err != nil {
		return StorageError(c, err)
	}

	ds, err := h.Store.DeviceStats(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error(), Code: "INTERNAL_ERROR"})
	}

	devices := make([]DeviceStatsResponse, len(ds.Devices))
	for i, d := range ds.Devices {
		devices[i] = DeviceStatsResponse{
			DevID:          d.DevID,
			Device:         d.Device,
			Missing:        d.Missing,
			SizeBytes:      d.SizeBytes,
			AllocatedBytes: d.AllocatedBytes,
			IO: DeviceIOStatsResponse{
				ReadBytesTotal:        d.IO.ReadBytes,
				ReadIOsTotal:          d.IO.ReadIOs,
				ReadTimeMsTotal:       d.IO.ReadTimeMs,
				WriteBytesTotal:       d.IO.WriteBytes,
				WriteIOsTotal:         d.IO.WriteIOs,
				WriteTimeMsTotal:      d.IO.WriteTimeMs,
				IOsInProgress:         d.IO.IOsInProgress,
				IOTimeMsTotal:         d.IO.IOTimeMs,
				WeightedIOTimeMsTotal: d.IO.WeightedIOTimeMs,
			},
			Errors: DeviceErrorsResponse{
				ReadErrs:       d.Errors.ReadErrs,
				WriteErrs:      d.Errors.WriteErrs,
				FlushErrs:      d.Errors.FlushErrs,
				CorruptionErrs: d.Errors.CorruptionErrs,
				GenerationErrs: d.Errors.GenerationErrs,
			},
		}
	}

	return c.JSON(http.StatusOK, StatsResponse{
		Statfs: StatfsResponse{
			TotalBytes: fs.TotalBytes,
			UsedBytes:  fs.UsedBytes,
			FreeBytes:  fs.FreeBytes,
		},
		Btrfs: FilesystemStatsResponse{
			TotalBytes:         ds.Filesystem.TotalBytes,
			UsedBytes:          ds.Filesystem.UsedBytes,
			FreeBytes:          ds.Filesystem.FreeBytes,
			UnallocatedBytes:   ds.Filesystem.UnallocatedBytes,
			MetadataUsedBytes:  ds.Filesystem.MetadataUsedBytes,
			MetadataTotalBytes: ds.Filesystem.MetadataTotalBytes,
			DataRatio:          ds.Filesystem.DataRatio,
			Devices:            devices,
		},
	})
}

// --- Snapshots ---

func snapshotResponseFrom(meta *storage.SnapshotMetadata) SnapshotResponse {
	return SnapshotResponse{
		Name:      meta.Name,
		Volume:    meta.Volume,
		SizeBytes: meta.SizeBytes,
		UsedBytes: meta.UsedBytes,
		CreatedAt: meta.CreatedAt,
	}
}

func (h *Handler) CreateSnapshot(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.SnapshotCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body", Code: "BAD_REQUEST"})
	}

	meta, err := h.Store.CreateSnapshot(c.Request().Context(), tenant, req)
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, snapshotDetailResponseFrom(meta))
}

func (h *Handler) ListSnapshots(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	snaps, err := h.Store.ListSnapshots(tenant, "")
	if err != nil {
		return StorageError(c, err)
	}

	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		snaps = filterByLabels(snaps, filters)
	}

	if c.QueryParam("detail") == "true" {
		resp := make([]SnapshotDetailResponse, len(snaps))
		for i := range snaps {
			resp[i] = snapshotDetailResponseFrom(&snaps[i])
		}
		return c.JSON(http.StatusOK, SnapshotDetailListResponse{Snapshots: resp, Total: len(resp)})
	}

	resp := make([]SnapshotResponse, len(snaps))
	for i := range snaps {
		resp[i] = snapshotResponseFrom(&snaps[i])
	}

	return c.JSON(http.StatusOK, SnapshotListResponse{Snapshots: resp, Total: len(resp)})
}

func (h *Handler) ListVolumeSnapshots(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	snaps, err := h.Store.ListSnapshots(tenant, c.Param("name"))
	if err != nil {
		return StorageError(c, err)
	}

	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		snaps = filterByLabels(snaps, filters)
	}

	if c.QueryParam("detail") == "true" {
		resp := make([]SnapshotDetailResponse, len(snaps))
		for i := range snaps {
			resp[i] = snapshotDetailResponseFrom(&snaps[i])
		}
		return c.JSON(http.StatusOK, SnapshotDetailListResponse{Snapshots: resp, Total: len(resp)})
	}

	resp := make([]SnapshotResponse, len(snaps))
	for i := range snaps {
		resp[i] = snapshotResponseFrom(&snaps[i])
	}

	return c.JSON(http.StatusOK, SnapshotListResponse{Snapshots: resp, Total: len(resp)})
}

func snapshotDetailResponseFrom(meta *storage.SnapshotMetadata) SnapshotDetailResponse {
	return SnapshotDetailResponse{
		Name:           meta.Name,
		Volume:         meta.Volume,
		Path:           meta.Path,
		SizeBytes:      meta.SizeBytes,
		UsedBytes:      meta.UsedBytes,
		ExclusiveBytes: meta.ExclusiveBytes,
		ReadOnly:       meta.ReadOnly,
		Labels:         meta.Labels,
		CreatedAt:      meta.CreatedAt,
		UpdatedAt:      meta.UpdatedAt,
	}
}

func (h *Handler) GetSnapshot(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	meta, err := h.Store.GetSnapshot(tenant, c.Param("name"))
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, snapshotDetailResponseFrom(meta))
}

func (h *Handler) DeleteSnapshot(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	if err := h.Store.DeleteSnapshot(c.Request().Context(), tenant, c.Param("name")); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

// --- Volume Clone (PVC-to-PVC) ---

func (h *Handler) CloneVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.VolumeCloneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body", Code: "BAD_REQUEST"})
	}

	meta, err := h.Store.CloneVolume(c.Request().Context(), tenant, req)
	if err != nil {
		if meta != nil {
			return c.JSON(http.StatusConflict, volumeDetailResponseFrom(meta))
		}
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, volumeDetailResponseFrom(meta))
}

// --- Clones ---

func (h *Handler) CreateClone(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.CloneCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body", Code: "BAD_REQUEST"})
	}

	meta, err := h.Store.CreateClone(c.Request().Context(), tenant, req)
	if err != nil {
		if meta != nil {
			return c.JSON(http.StatusConflict, cloneResponseFrom(meta))
		}
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, cloneResponseFrom(meta))
}

func cloneResponseFrom(meta *storage.CloneMetadata) CloneResponse {
	return CloneResponse{
		Name:           meta.Name,
		SourceSnapshot: meta.SourceSnapshot,
		Path:           meta.Path,
		Labels:         meta.Labels,
		CreatedAt:      meta.CreatedAt,
	}
}

// --- Tasks ---

func (h *Handler) CreateTask(c *echo.Context) error {
	taskType := c.Param("type")

	var req TaskCreateRequest
	if c.Request().ContentLength > 0 || c.Request().ContentLength == -1 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body: " + err.Error(), Code: storage.ErrInvalid})
		}
	}

	var timeout time.Duration
	if req.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(req.Timeout)
		if err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid timeout: " + req.Timeout, Code: storage.ErrInvalid})
		}
	}

	var taskID string
	var err error
	switch taskType {
	case TaskTypeScrub:
		taskID, err = h.Store.StartScrub(c.Request().Context(), req.Opts, req.Labels, timeout)
	case TaskTypeTest:
		taskID, err = h.Store.StartTestTask(c.Request().Context(), req.Opts, req.Labels, timeout)
	default:
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unknown task type: " + taskType, Code: storage.ErrInvalid})
	}
	if err != nil {
		return StorageError(c, err)
	}
	return c.JSON(http.StatusAccepted, TaskCreateResponse{TaskID: taskID, Status: TaskStatusPending})
}

func (h *Handler) ListTasks(c *echo.Context) error {
	tasks := h.Store.Tasks().List(c.QueryParam("type"))

	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		tasks = filterByLabels(tasks, filters)
	}

	if c.QueryParam("detail") == "true" {
		detail := make([]TaskDetailResponse, len(tasks))
		for i, t := range tasks {
			detail[i] = taskDetailResponseFrom(&t)
		}
		return c.JSON(http.StatusOK, TaskDetailListResponse{Tasks: detail, Total: len(detail)})
	}

	resp := make([]TaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskResponseFrom(&t)
	}
	return c.JSON(http.StatusOK, TaskListResponse{Tasks: resp, Total: len(resp)})
}

func (h *Handler) GetTask(c *echo.Context) error {
	task, err := h.Store.Tasks().Get(c.Param("id"))
	if err != nil {
		return StorageError(c, err)
	}
	return c.JSON(http.StatusOK, taskDetailResponseFrom(task))
}

func (h *Handler) CancelTask(c *echo.Context) error {
	if err := h.Store.Tasks().Cancel(c.Param("id")); err != nil {
		return StorageError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
