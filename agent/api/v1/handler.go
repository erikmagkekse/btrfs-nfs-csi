package v1

import (
	"net/http"
	"strconv"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"

	"github.com/labstack/echo/v5"
)

func parsePageParams(c *echo.Context) (after string, limit int) {
	after = c.QueryParam("after")
	if v := c.QueryParam("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
		if limit < 0 {
			limit = 0
		}
	}
	return
}

type Handler struct {
	Store *storage.Storage
}

// --- Volumes ---

func volumeResponseFrom(meta *storage.VolumeMetadata) VolumeResponse {
	return VolumeResponse{
		Name:      meta.Name,
		SizeBytes: meta.SizeBytes,
		UsedBytes: meta.UsedBytes,
		Exports:   storage.CountUniqueExportIPs(meta.Exports),
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
	after, limit := parsePageParams(c)

	page, err := h.Store.ListVolumesPaginated(tenant, after, limit)
	if err != nil {
		return StorageError(c, err)
	}

	vols := page.Items
	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		vols = filterByLabels(vols, filters)
	}

	if c.QueryParam("detail") == "true" {
		resp := make([]VolumeDetailResponse, len(vols))
		for i := range vols {
			resp[i] = volumeDetailResponseFrom(&vols[i])
		}
		return c.JSON(http.StatusOK, VolumeDetailListResponse{Volumes: resp, Total: page.Total, Next: page.Next})
	}

	resp := make([]VolumeResponse, len(vols))
	for i := range vols {
		resp[i] = volumeResponseFrom(&vols[i])
	}

	return c.JSON(http.StatusOK, VolumeListResponse{Volumes: resp, Total: page.Total, Next: page.Next})
}

func volumeDetailResponseFrom(meta *storage.VolumeMetadata) VolumeDetailResponse {
	exports := make([]ExportDetailResponse, len(meta.Exports))
	for i, e := range meta.Exports {
		exports[i] = ExportDetailResponse{
			Name:      meta.Name,
			Client:    e.IP,
			Labels:    e.Labels,
			CreatedAt: e.CreatedAt,
		}
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
		Exports:      exports,
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

func (h *Handler) CreateVolumeExport(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req VolumeExportCreateRequest
	if err := c.Bind(&req); err != nil || req.Client == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "client is required", Code: "BAD_REQUEST"})
	}

	if err := h.Store.CreateVolumeExport(c.Request().Context(), tenant, name, req.Client, req.Labels); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeleteVolumeExport(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req VolumeExportDeleteRequest
	if err := c.Bind(&req); err != nil || req.Client == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "client is required", Code: "BAD_REQUEST"})
	}

	if err := h.Store.DeleteVolumeExport(c.Request().Context(), tenant, name, req.Client, req.Labels); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func exportResponseFrom(e *storage.ExportEntry) ExportResponse {
	return ExportResponse{Name: e.Name, Client: e.Client, CreatedAt: e.CreatedAt}
}

func exportDetailResponseFrom(e *storage.ExportEntry) ExportDetailResponse {
	return ExportDetailResponse{Name: e.Name, Client: e.Client, Labels: e.Labels, CreatedAt: e.CreatedAt}
}

func (h *Handler) ListVolumeExports(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	after, limit := parsePageParams(c)

	page, err := h.Store.ListVolumeExportsPaginated(tenant, after, limit)
	if err != nil {
		return StorageError(c, err)
	}

	items := page.Items
	total := page.Total
	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		items = filterByLabels(items, filters)
		total = len(items)
	}

	if c.QueryParam("detail") == "true" {
		resp := make([]ExportDetailResponse, len(items))
		for i := range items {
			resp[i] = exportDetailResponseFrom(&items[i])
		}
		return c.JSON(http.StatusOK, ExportDetailListResponse{Exports: resp, Total: total, Next: page.Next})
	}

	resp := make([]ExportResponse, len(items))
	for i := range items {
		resp[i] = exportResponseFrom(&items[i])
	}
	return c.JSON(http.StatusOK, ExportListResponse{Exports: resp, Total: total, Next: page.Next})
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
		TenantName: tenant,
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

func (h *Handler) listSnapshotsPage(c *echo.Context, volume string) error {
	tenant := c.Get("tenant").(string)
	after, limit := parsePageParams(c)

	page, err := h.Store.ListSnapshotsPaginated(tenant, volume, after, limit)
	if err != nil {
		return StorageError(c, err)
	}

	snaps := page.Items
	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		snaps = filterByLabels(snaps, filters)
	}

	if c.QueryParam("detail") == "true" {
		resp := make([]SnapshotDetailResponse, len(snaps))
		for i := range snaps {
			resp[i] = snapshotDetailResponseFrom(&snaps[i])
		}
		return c.JSON(http.StatusOK, SnapshotDetailListResponse{Snapshots: resp, Total: page.Total, Next: page.Next})
	}

	resp := make([]SnapshotResponse, len(snaps))
	for i := range snaps {
		resp[i] = snapshotResponseFrom(&snaps[i])
	}

	return c.JSON(http.StatusOK, SnapshotListResponse{Snapshots: resp, Total: page.Total, Next: page.Next})
}

func (h *Handler) ListSnapshots(c *echo.Context) error {
	return h.listSnapshotsPage(c, "")
}

func (h *Handler) ListVolumeSnapshots(c *echo.Context) error {
	return h.listSnapshotsPage(c, c.Param("name"))
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
			return c.JSON(http.StatusConflict, volumeDetailResponseFrom(meta))
		}
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, volumeDetailResponseFrom(meta))
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
	after, limit := parsePageParams(c)
	tasks, total, next := h.Store.Tasks().ListPaginated(c.QueryParam("type"), after, limit)

	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		tasks = filterByLabels(tasks, filters)
	}

	if c.QueryParam("detail") == "true" {
		detail := make([]TaskDetailResponse, len(tasks))
		for i, t := range tasks {
			detail[i] = taskDetailResponseFrom(&t)
		}
		return c.JSON(http.StatusOK, TaskDetailListResponse{Tasks: detail, Total: total, Next: next})
	}

	resp := make([]TaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskResponseFrom(&t)
	}
	return c.JSON(http.StatusOK, TaskListResponse{Tasks: resp, Total: total, Next: next})
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
