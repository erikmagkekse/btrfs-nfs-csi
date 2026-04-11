package v1

import (
	"cmp"
	"net/http"
	"slices"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/labstack/echo/v5"
)

func volumeResponseFrom(meta *storage.VolumeMetadata) models.VolumeResponse {
	return models.VolumeResponse{
		Name:      meta.Name,
		CreatedBy: meta.Labels[config.LabelCreatedBy],
		SizeBytes: meta.SizeBytes,
		UsedBytes: meta.UsedBytes,
		Exports:   storage.CountUniqueExportIPs(meta.Exports),
		CreatedAt: meta.CreatedAt,
	}
}

func volumeDetailResponseFrom(meta *storage.VolumeMetadata) models.VolumeDetailResponse {
	exports := make([]models.ExportDetailResponse, len(meta.Exports))
	for i, e := range meta.Exports {
		exports[i] = models.ExportDetailResponse{
			Name:      meta.Name,
			Client:    e.IP,
			Labels:    e.Labels,
			CreatedAt: e.CreatedAt,
		}
	}
	return models.VolumeDetailResponse{
		Name:         meta.Name,
		CreatedBy:    meta.Labels[config.LabelCreatedBy],
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

// CreateVolume godoc
// @Summary      Create a volume
// @Description  Creates a new btrfs subvolume. Returns the existing volume on conflict.
// @Tags         volumes
// @Accept       json
// @Produce      json
// @Param        request body models.VolumeCreateRequest true "Volume configuration"
// @Success      201 {object} models.VolumeDetailResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.VolumeDetailResponse "Volume already exists"
// @Router       /v1/volumes [post]
// @Security     BearerAuth
func (h *Handler) CreateVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.VolumeCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body", Code: storage.ErrBadRequest})
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

// ListVolumes godoc
// @Summary      List volumes
// @Description  Returns all volumes. Use detail=true for full metadata. Supports pagination and label filtering.
// @Tags         volumes
// @Produce      json
// @Param        detail query string false "Return full detail" Enums(true)
// @Param        limit  query int    false "Items per page (0 = pagination disabled)"
// @Param        after  query string false "Pagination cursor from previous response"
// @Param        label  query []string false "Label filter (key=value), repeatable"
// @Success      200 {object} models.VolumeListResponse
// @Router       /v1/volumes [get]
// @Security     BearerAuth
func (h *Handler) ListVolumes(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	vols, err := h.Store.ListVolumes(tenant)
	if err != nil {
		return StorageError(c, err)
	}
	slices.SortFunc(vols, func(a, b storage.VolumeMetadata) int { return cmp.Compare(a.Name, b.Name) })

	if c.QueryParam("detail") == "true" {
		return paginatedList(h, c, vols, volumeDetailResponseFrom, func(r []models.VolumeDetailResponse, total int, next string) any {
			return models.VolumeDetailListResponse{Volumes: r, Total: total, Next: next}
		})
	}
	return paginatedList(h, c, vols, volumeResponseFrom, func(r []models.VolumeResponse, total int, next string) any {
		return models.VolumeListResponse{Volumes: r, Total: total, Next: next}
	})
}

// GetVolume godoc
// @Summary      Get a volume
// @Description  Returns detailed metadata for a single volume.
// @Tags         volumes
// @Produce      json
// @Param        name path string true "Volume name"
// @Success      200 {object} models.VolumeDetailResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/volumes/{name} [get]
// @Security     BearerAuth
func (h *Handler) GetVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	meta, err := h.Store.GetVolume(tenant, c.Param("name"))
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, volumeDetailResponseFrom(meta))
}

// UpdateVolume godoc
// @Summary      Update a volume
// @Description  Patches volume properties. Nil/omitted fields are left unchanged.
// @Tags         volumes
// @Accept       json
// @Produce      json
// @Param        name    path string true "Volume name"
// @Param        request body models.VolumeUpdateRequest true "Fields to update"
// @Success      200 {object} models.VolumeDetailResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/volumes/{name} [patch]
// @Security     BearerAuth
func (h *Handler) UpdateVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req storage.VolumeUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body", Code: storage.ErrBadRequest})
	}

	meta, err := h.Store.UpdateVolume(c.Request().Context(), tenant, name, req)
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, volumeDetailResponseFrom(meta))
}

// DeleteVolume godoc
// @Summary      Delete a volume
// @Description  Deletes a volume and its btrfs subvolume. Fails if the volume has active exports.
// @Tags         volumes
// @Param        name path string true "Volume name"
// @Success      204 "No Content"
// @Failure      404 {object} models.ErrorResponse
// @Failure      423 {object} models.ErrorResponse "Volume has active exports"
// @Router       /v1/volumes/{name} [delete]
// @Security     BearerAuth
func (h *Handler) DeleteVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	if err := h.Store.DeleteVolume(c.Request().Context(), tenant, c.Param("name")); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

// --- Clones ---

// CloneVolume godoc
// @Summary      Clone a volume
// @Description  Creates a new volume from another volume (internal snapshot + clone). Returns existing on conflict.
// @Tags         clones
// @Accept       json
// @Produce      json
// @Param        request body models.VolumeCloneRequest true "Clone configuration"
// @Success      201 {object} models.VolumeDetailResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.VolumeDetailResponse "Volume already exists"
// @Router       /v1/volumes/clone [post]
// @Security     BearerAuth
func (h *Handler) CloneVolume(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.VolumeCloneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body", Code: storage.ErrBadRequest})
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

// CreateClone godoc
// @Summary      Create a clone from snapshot
// @Description  Creates a new volume from a snapshot. Returns existing on conflict.
// @Tags         clones
// @Accept       json
// @Produce      json
// @Param        request body models.CloneCreateRequest true "Clone configuration"
// @Success      201 {object} models.VolumeDetailResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.VolumeDetailResponse "Volume already exists"
// @Router       /v1/clones [post]
// @Security     BearerAuth
func (h *Handler) CreateClone(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.CloneCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body", Code: storage.ErrBadRequest})
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
