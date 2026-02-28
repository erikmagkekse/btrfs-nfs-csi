package v1

import (
	"net/http"

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

	stats, err := h.Store.Stats(tenant)
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, StatsResponse{
		TotalBytes: stats.TotalBytes,
		UsedBytes:  stats.UsedBytes,
		FreeBytes:  stats.FreeBytes,
	})
}

// --- Snapshots ---

func snapshotResponseFrom(meta *storage.SnapshotMetadata) SnapshotResponse {
	return SnapshotResponse{
		Name:      meta.Name,
		Volume:    meta.Volume,
		SizeBytes: meta.SizeBytes,
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
			return c.JSON(http.StatusConflict, CloneResponse{
				Name:           meta.Name,
				SourceSnapshot: meta.SourceSnapshot,
				Path:           meta.Path,
				CreatedAt:      meta.CreatedAt,
			})
		}
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, CloneResponse{
		Name:           meta.Name,
		SourceSnapshot: meta.SourceSnapshot,
		Path:           meta.Path,
		CreatedAt:      meta.CreatedAt,
	})
}
