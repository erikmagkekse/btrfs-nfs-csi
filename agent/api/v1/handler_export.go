package v1

import (
	"net/http"
	"sort"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/labstack/echo/v5"
)

func exportResponseFrom(e *storage.ExportEntry) models.ExportResponse {
	return models.ExportResponse{Name: e.Name, CreatedBy: e.Labels[config.LabelCreatedBy], Client: e.Client, CreatedAt: e.CreatedAt}
}

func exportDetailResponseFrom(e *storage.ExportEntry) models.ExportDetailResponse {
	return models.ExportDetailResponse{Name: e.Name, CreatedBy: e.Labels[config.LabelCreatedBy], Client: e.Client, Labels: e.Labels, CreatedAt: e.CreatedAt}
}

// CreateVolumeExport godoc
// @Summary      Add NFS export
// @Description  Adds an NFS export for a volume to the given client IP.
// @Tags         exports
// @Accept       json
// @Param        name    path string true "Volume name"
// @Param        request body models.VolumeExportCreateRequest true "Export config"
// @Success      204 "No Content"
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/volumes/{name}/export [post]
// @Security     BearerAuth
func (h *Handler) CreateVolumeExport(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req models.VolumeExportCreateRequest
	if err := c.Bind(&req); err != nil || req.Client == "" {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "client is required", Code: storage.ErrBadRequest})
	}

	if err := h.Store.CreateVolumeExport(c.Request().Context(), tenant, name, req.Client, req.Labels); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

// DeleteVolumeExport godoc
// @Summary      Remove NFS export
// @Description  Removes an NFS export for a volume from the given client IP.
// @Tags         exports
// @Accept       json
// @Param        name    path string true "Volume name"
// @Param        request body models.VolumeExportDeleteRequest true "Export to remove"
// @Success      204 "No Content"
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/volumes/{name}/export [delete]
// @Security     BearerAuth
func (h *Handler) DeleteVolumeExport(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	name := c.Param("name")

	var req models.VolumeExportDeleteRequest
	if err := c.Bind(&req); err != nil || req.Client == "" {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "client is required", Code: storage.ErrBadRequest})
	}

	if err := h.Store.DeleteVolumeExport(c.Request().Context(), tenant, name, req.Client, req.Labels); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

// ListVolumeExports godoc
// @Summary      List NFS exports
// @Description  Returns all active NFS exports. Use detail=true for labels. Supports pagination and label filtering.
// @Tags         exports
// @Produce      json
// @Param        detail query string false "Return full detail" Enums(true)
// @Param        limit  query int    false "Items per page"
// @Param        after  query string false "Pagination cursor"
// @Param        label  query []string false "Label filter (key=value), repeatable"
// @Success      200 {object} models.ExportListResponse
// @Router       /v1/exports [get]
// @Security     BearerAuth
func (h *Handler) ListVolumeExports(c *echo.Context) error {
	tenant := c.Get("tenant").(string)
	items, err := h.Store.ListVolumeExports(tenant)
	if err != nil {
		return StorageError(c, err)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Client < items[j].Client
	})

	if c.QueryParam("detail") == "true" {
		return paginatedList(h, c, items, exportDetailResponseFrom, func(r []models.ExportDetailResponse, total int, next string) any {
			return models.ExportDetailListResponse{Exports: r, Total: total, Next: next}
		})
	}
	return paginatedList(h, c, items, exportResponseFrom, func(r []models.ExportResponse, total int, next string) any {
		return models.ExportListResponse{Exports: r, Total: total, Next: next}
	})
}
