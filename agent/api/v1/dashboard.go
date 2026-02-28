package v1

import (
	_ "embed"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
)

//go:embed dashboard.html
var dashboardHTML string

func ServeDashboard(refreshSeconds int) echo.HandlerFunc {
	refresh := strconv.Itoa(refreshSeconds)
	return func(c *echo.Context) error {
		tenant := c.Get("tenant").(string)
		display := ""
		if tenant != "" {
			display = " &mdash; " + tenant
		}
		r := strings.NewReplacer("{{TENANT}}", display, "{{REFRESH}}", refresh)
		html := r.Replace(dashboardHTML)
		c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		return c.HTML(http.StatusOK, html)
	}
}
