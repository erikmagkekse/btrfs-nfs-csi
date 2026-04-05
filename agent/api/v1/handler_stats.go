package v1

import (
	"net/http"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
)

// Stats godoc
// @Summary      Get filesystem statistics
// @Description  Returns statfs and btrfs device/filesystem statistics.
// @Tags         stats
// @Produce      json
// @Success      200 {object} models.StatsResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /v1/stats [get]
// @Security     BearerAuth
func (h *Handler) Stats(c *echo.Context) error {
	tenant := c.Get("tenant").(string)

	fs, err := h.Store.Stats(tenant)
	if err != nil {
		return StorageError(c, err)
	}

	ds, err := h.Store.DeviceStats(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: err.Error(), Code: storage.ErrInternal})
	}

	devices := make([]models.DeviceStatsResponse, len(ds.Devices))
	for i, d := range ds.Devices {
		devices[i] = models.DeviceStatsResponse{
			DevID:          d.DevID,
			Device:         d.Device,
			Missing:        d.Missing,
			SizeBytes:      d.SizeBytes,
			AllocatedBytes: d.AllocatedBytes,
			IO: models.DeviceIOStatsResponse{
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
			Errors: models.DeviceErrorsResponse{
				ReadErrs:       d.Errors.ReadErrs,
				WriteErrs:      d.Errors.WriteErrs,
				FlushErrs:      d.Errors.FlushErrs,
				CorruptionErrs: d.Errors.CorruptionErrs,
				GenerationErrs: d.Errors.GenerationErrs,
			},
		}
	}

	return c.JSON(http.StatusOK, models.StatsResponse{
		TenantName: tenant,
		Statfs: models.StatfsResponse{
			TotalBytes: fs.TotalBytes,
			UsedBytes:  fs.UsedBytes,
			FreeBytes:  fs.FreeBytes,
		},
		Btrfs: models.FilesystemStatsResponse{
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
