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

func snapshotResponseFrom(meta *storage.SnapshotMetadata) models.SnapshotResponse {
	return models.SnapshotResponse{
		Name:      meta.Name,
		CreatedBy: meta.Labels[config.LabelCreatedBy],
		Volume:    meta.Volume,
		SizeBytes: meta.SizeBytes,
		UsedBytes: meta.UsedBytes,
		CreatedAt: meta.CreatedAt,
	}
}

func snapshotDetailResponseFrom(meta *storage.SnapshotMetadata) models.SnapshotDetailResponse {
	return models.SnapshotDetailResponse{
		Name:           meta.Name,
		CreatedBy:      meta.Labels[config.LabelCreatedBy],
		Volume:         meta.Volume,
		Path:           meta.Path,
		SizeBytes:      meta.SizeBytes,
		UsedBytes:      meta.UsedBytes,
		ExclusiveBytes: meta.ExclusiveBytes,
		QuotaBytes:     meta.QuotaBytes,
		NoCOW:          meta.NoCOW,
		Compression:    meta.Compression,
		UID:            meta.UID,
		GID:            meta.GID,
		Mode:           meta.Mode,
		Labels:         meta.Labels,
		CreatedAt:      meta.CreatedAt,
		UpdatedAt:      meta.UpdatedAt,
	}
}

// CreateSnapshot godoc
// @Summary      Create a snapshot
// @Description  Creates a read-only btrfs snapshot of a volume. Returns existing snapshot on conflict.
// @Tags         snapshots
// @Accept       json
// @Produce      json
// @Param        request body models.SnapshotCreateRequest true "Snapshot configuration"
// @Success      201 {object} models.SnapshotDetailResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.SnapshotDetailResponse "Snapshot already exists"
// @Router       /v1/snapshots [post]
// @Security     BearerAuth
func (h *Handler) CreateSnapshot(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	var req storage.SnapshotCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body", Code: storage.ErrBadRequest})
	}

	meta, err := h.Store.CreateSnapshot(c.Request().Context(), tenant, req)
	if err != nil {
		if meta != nil {
			return c.JSON(http.StatusConflict, snapshotDetailResponseFrom(meta))
		}
		return StorageError(c, err)
	}

	return c.JSON(http.StatusCreated, snapshotDetailResponseFrom(meta))
}

func (h *Handler) listSnapshotsPage(c *echo.Context, volume string) error {
	tenant := c.Get("tenant").(string)
	snaps, err := h.Store.ListSnapshots(tenant, volume)
	if err != nil {
		return StorageError(c, err)
	}
	slices.SortFunc(snaps, func(a, b storage.SnapshotMetadata) int { return cmp.Compare(a.Name, b.Name) })

	if c.QueryParam("detail") == "true" {
		return paginatedList(h, c, snaps, snapshotDetailResponseFrom, func(r []models.SnapshotDetailResponse, total int, next string) any {
			return models.SnapshotDetailListResponse{Snapshots: r, Total: total, Next: next}
		})
	}
	return paginatedList(h, c, snaps, snapshotResponseFrom, func(r []models.SnapshotResponse, total int, next string) any {
		return models.SnapshotListResponse{Snapshots: r, Total: total, Next: next}
	})
}

// ListSnapshots godoc
// @Summary      List snapshots
// @Description  Returns all snapshots. Use detail=true for full metadata. Supports pagination and label filtering.
// @Tags         snapshots
// @Produce      json
// @Param        detail query string false "Return full detail" Enums(true)
// @Param        limit  query int    false "Items per page (0 = pagination disabled)"
// @Param        after  query string false "Pagination cursor"
// @Param        label  query []string false "Label filter (key=value), repeatable"
// @Success      200 {object} models.SnapshotListResponse
// @Router       /v1/snapshots [get]
// @Security     BearerAuth
func (h *Handler) ListSnapshots(c *echo.Context) error {
	return h.listSnapshotsPage(c, "")
}

// ListVolumeSnapshots godoc
// @Summary      List snapshots for a volume
// @Description  Returns snapshots for a specific volume. Supports detail, pagination, and label filtering.
// @Tags         snapshots
// @Produce      json
// @Param        name   path   string false "Volume name"
// @Param        detail query  string false "Return full detail" Enums(true)
// @Param        limit  query  int    false "Items per page"
// @Param        after  query  string false "Pagination cursor"
// @Param        label  query  []string false "Label filter (key=value), repeatable"
// @Success      200 {object} models.SnapshotListResponse
// @Router       /v1/volumes/{name}/snapshots [get]
// @Security     BearerAuth
func (h *Handler) ListVolumeSnapshots(c *echo.Context) error {
	return h.listSnapshotsPage(c, c.Param("name"))
}

// GetSnapshot godoc
// @Summary      Get a snapshot
// @Description  Returns detailed metadata for a single snapshot.
// @Tags         snapshots
// @Produce      json
// @Param        name path string true "Snapshot name"
// @Success      200 {object} models.SnapshotDetailResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/snapshots/{name} [get]
// @Security     BearerAuth
func (h *Handler) GetSnapshot(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	meta, err := h.Store.GetSnapshot(tenant, c.Param("name"))
	if err != nil {
		return StorageError(c, err)
	}

	return c.JSON(http.StatusOK, snapshotDetailResponseFrom(meta))
}

// DeleteSnapshot godoc
// @Summary      Delete a snapshot
// @Description  Deletes a snapshot and its btrfs subvolume.
// @Tags         snapshots
// @Param        name path string true "Snapshot name"
// @Success      204 "No Content"
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/snapshots/{name} [delete]
// @Security     BearerAuth
func (h *Handler) DeleteSnapshot(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	if err := h.Store.DeleteSnapshot(c.Request().Context(), tenant, c.Param("name")); err != nil {
		return StorageError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}
