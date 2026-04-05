package v1

import (
	"net/http"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
)

// Healthz godoc
// @Summary      Health check
// @Description  Returns agent health status, version, and enabled features. Does not require authentication.
// @Tags         health
// @Produce      json
// @Success      200 {object} models.HealthResponse
// @Router       /healthz [get]
func Healthz(version, commit string, store *storage.Storage) echo.HandlerFunc {
	startTime := time.Now()

	return func(c *echo.Context) error {
		status := models.HealthStatusOK
		if store.IsDegraded() {
			status = models.HealthStatusDegraded
		}

		return c.JSON(http.StatusOK, models.HealthResponse{
			Status:        status,
			Version:       version,
			Commit:        commit,
			UptimeSeconds: int(time.Since(startTime).Seconds()),
		})
	}
}
