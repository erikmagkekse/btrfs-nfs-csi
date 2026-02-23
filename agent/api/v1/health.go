package v1

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

func Healthz(version, commit string, features map[string]string) echo.HandlerFunc {
	startTime := time.Now()

	return func(c *echo.Context) error {
		return c.JSON(http.StatusOK, HealthResponse{
			Status:        "ok", // Maybe for the future we can add more status levels like "degraded"
			Version:       version,
			Commit:        commit,
			UptimeSeconds: int(time.Since(startTime).Seconds()),
			Features:      features,
		})
	}
}
