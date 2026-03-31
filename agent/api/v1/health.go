package v1

import (
	"net/http"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
)

func Healthz(version, commit string, features map[string]string, store *storage.Storage) echo.HandlerFunc {
	startTime := time.Now()

	return func(c *echo.Context) error {
		status := HealthStatusOK
		if store.IsDegraded() {
			status = HealthStatusDegraded
		}

		return c.JSON(http.StatusOK, HealthResponse{
			Status:        status,
			Version:       version,
			Commit:        commit,
			UptimeSeconds: int(time.Since(startTime).Seconds()),
			Features:      features,
		})
	}
}
